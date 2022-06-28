package daemon

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log"
	"os"
	"os/exec"
	"runtime"
	"time"
)

// Status enum type for running or stopped process
type Status int

const (
	// Stopped means the process is stopped
	Stopped Status = iota
	// Starting means the process is starting
	Starting
	// Running means the process is running
	Running
)

// Daemon is a wrapper for a process
type Daemon struct {
	// Context for the daemon
	Context context.Context
	// Cwd is the working directory of the process
	Cwd string
	// Command for running the process
	Cmd string
	// Status of the process
	Status     Status
	cmd        *exec.Cmd
	StartDelay time.Duration
}

// Return a new daemon
func NewDaemon(cwd, cmd string) *Daemon {
	return &Daemon{
		Cwd:        cwd,
		Cmd:        cmd,
		Status:     Stopped,
		StartDelay: 0,
	}
}

func (d *Daemon) Start() error {
	log.Println("Starting daemon")

	// FIXME: This doesn't work, because d.cmd.ProcessState is ALWAYS nil for some reason..
	// Ensure the process is stopped
	// if !d.Exited() {
	// if !d.cmd.ProcessState.Exited() {
	if d.Status != Stopped {
		return errors.New("unable to start daemon, already running")
	}

	d.Status = Starting

	// Create a new command and launch it using the default shell for the current platform
	// d.cmd = exec.Command("/bin/bash", "-c", d.Cmd)
	if runtime.GOOS == "windows" {
		d.cmd = exec.Command("cmd", "/c", d.Cmd)
	} else {
		d.cmd = exec.Command("/bin/sh", "-c", d.Cmd)
	}
	d.cmd.Dir = d.Cwd

	// Attempt to open the process stdin and stderr pipes
	stderr, err := d.cmd.StderrPipe()
	if err != nil {
		log.Printf("Error getting daemon stdout pipe: %s", err)
		d.Status = Stopped
		return err
	}
	stdout, err := d.cmd.StdoutPipe()
	if err != nil {
		log.Printf("Error getting daemon stdout pipe: %s", err)
		d.Status = Stopped
		return err
	}

	// TODO: Only show stdout if DEBUG mode is enabled?!
	// Handle the process's stdin and stderr pipes in separate goroutines
	go handleOutput(d, stdout, false)
	go handleOutput(d, stderr, true)

	// Attempt to start the process
	if err := d.cmd.Start(); err != nil {
		log.Println("Error starting daemon:", err)
		d.Status = Stopped
		return err
	}

	// FIXME: This doesn't work, because d.cmd.ProcessState is ALWAYS nil for some reason..
	// Ensure the process is started
	// for d.Exited() {
	// for d.cmd.ProcessState.Exited() {
	// for d.Status != Running {
	// 	log.Println("Waiting for daemon to start, current status:", d.Status)
	// 	time.Sleep(time.Second)
	// }

	// Apply a startup delay if enabled
	if d.StartDelay > 0 {
		log.Printf("Delaying daemon post-startup by %f seconds ...", d.StartDelay.Seconds())
		time.Sleep(d.StartDelay)
	}

	d.Status = Running
	return nil
}

// func (d *Daemon) Exited() bool {
// 	if d.cmd == nil {
// 		log.Println("Daemon command is nil")
// 	} else {
// 		if d.cmd.Process == nil {
// 			log.Println("Daemon command process is nil")
// 		}
// 		if d.cmd.ProcessState == nil {
// 			log.Println("Daemon command process state is nil")
// 			// FIXME: What if we just return true here?
// 			return true
// 			// if d.cmd.Process != nil {
// 			// 	log.Println("Daemon command process is running with pid", d.cmd.Process.Pid)
// 			// 	return false // FIXME: Why is the process state nil, but the process is still running?!
// 			// }
// 		}
// 	}

// 	if d.cmd != nil && d.cmd.ProcessState != nil {
// 		log.Println("Daemon exited with status:", d.cmd.ProcessState.String(), "and exit bool:", d.cmd.ProcessState.Exited(), "and success bool:", d.cmd.ProcessState.Success())
// 		return d.cmd.ProcessState.Exited() || d.cmd.ProcessState.Success()
// 	}

// 	log.Println("Returning true for exited")
// 	return true
// }

func (d *Daemon) Stop() error {
	log.Println("Stopping daemon")

	// FIXME: This doesn't work, because d.cmd.ProcessState is ALWAYS nil for some reason..
	// Ensure the process is running
	// if d.Exited() {
	// if d.cmd.ProcessState.Exited() {
	if d.Status == Stopped {
		return errors.New("unable to stop daemon, already stopped")
	}

	// Attempt to gracefully stop the process and fallback to a force kill if it doesn't stop
	if err := d.cmd.Process.Signal(os.Interrupt); err != nil {
		log.Println("Error stopping daemon:", err)
		// return err

		// Kill the process
		// if err := d.cmd.Process.Signal(os.Interrupt); err != nil {
		log.Println("Daemon is still running, killing it ...")
		if err := d.cmd.Process.Kill(); err != nil {
			// Ignore the error if the process is already dead
			if err.Error() != "os: process already finished" {
				log.Println("Error stopping daemon:", err)
				return err
			}
		}
	}

	// TODO: Should this be done before or after waiting for the command to finish?
	// Wait for the actual process to exit
	processState, err := d.cmd.Process.Wait()
	if err != nil {
		log.Println("Error waiting for daemon to stop:", err)
		return err
	}
	log.Println("Daemon process exited with state:", processState.String())

	// Wait for the command itself to finish
	if err := d.cmd.Wait(); err != nil {
		// FIXME: Error waiting for daemon to stop: waitid: no child processes
		// TODO: Maybe just ignore that error?
		// Ignore the error if the process was killed
		if err.Error() != "signal: killed" && err.Error() != "waitid: no child processes" {
			log.Println("Error waiting for daemon to stop:", err.Error())
			return err
		}
	}

	// TODO: Is this useless?
	if d.cmd.ProcessState != nil {
		log.Println("Daemon stopped with status:", d.cmd.ProcessState.String())
		// TODO: Check state and call process wait to wait for it to be killed?
	}

	// FIXME: We need to somehow be able to CONFIRM that the process has stopped..

	// FIXME: This doesn't work, because d.cmd.ProcessState is ALWAYS nil for some reason..
	// Ensure the process is stopped
	// for !d.Exited() {
	// for !d.cmd.ProcessState.Exited() {
	// for d.Status != Stopped {
	// 	log.Println("Waiting for daemon to stop, current status:", d.Status)
	// 	time.Sleep(time.Second)
	// }

	d.Status = Stopped
	return nil
}

func (d *Daemon) GetProcess() *os.Process {
	return d.cmd.Process
}

// func (d *Daemon) GetStatus() Status {
// 	log.Println("Getting daemon status")

// 	if (d.cmd == nil) || (d.cmd.Process == nil) {
// 		log.Println("Daemon or process is not running")
// 		return Stopped
// 	}

// 	log.Println("Checking if process is running with pid", d.cmd.Process.Pid)
// 	daemonProcess, err := os.FindProcess(d.cmd.Process.Pid)
// 	if err != nil {
// 		log.Fatal("Error finding daemon process:", err)
// 	}
// 	if daemonProcess == nil {
// 		log.Fatal("Daemon process was nil")
// 	}

// 	if d.Status == Stopped {
// 		// TODO: Are we sure the process is running? Because I don't think it is anymore..
// 		log.Println("Daemon status is stopped but process is running")
// 		state, err := daemonProcess.Wait()
// 		if err != nil {
// 			log.Fatal("Error waiting for daemon process:", err)
// 		}
// 		log.Println("Daemon process exited with status:", state.String())

// 		// TODO: We need to somehow wait here until the process is stopped?!
// 	}

// 	// log.Println("Daemon process is running, returning current status", d.Status)
// 	// return d.Status
// 	log.Println("Daemon process is running")
// 	return Running
// }

func (s Status) String() string {
	switch s {
	case Stopped:
		return "Stopped"
	case Starting:
		return "Starting"
	case Running:
		return "Running"
	default:
		return "Unknown"
	}
}

func handleOutput(d *Daemon, output io.ReadCloser, isErrorOutput bool) {
	// Create a new scanner for the output
	scanner := bufio.NewScanner(output)

	// Start processing the output
	for {
		select {
		// Handle context cancellation
		case <-d.Context.Done():
			if isErrorOutput {
				log.Println("Context done, stopping daemon error output handler")
			} else {
				log.Println("Context done, stopping daemon output handler")
			}
			return
		// Handle new output data
		default:
			// Bail out early if the process is no longer running
			if d.Status == Stopped {
				log.Println("Stopping daemon output handler, daemon is stopped")
				if isErrorOutput {
					log.Println("Stopping daemon error output handler, daemon is stopped")
				} else {
					log.Println("Stopping daemon normal output handler, daemon is stopped")
				}
				return
			}

			// Print the process output as long as it is still running
			if d.Status == Running {
				// Scan for the next chunk of output
				for scanner.Scan() {
					// Get the next chunk of output as a text string
					outputMessage := scanner.Text()

					if isErrorOutput {
						// Print the output as an error
						log.Println("[DAEMON STDERR]", outputMessage)
					} else {
						// Print the output as normal output
						log.Println("[DAEMON STDOUT]", outputMessage)
					}
				}

				// Handler errors produced by the scanner
				if err := scanner.Err(); err != nil {
					// Ignore the error if the process is already dead
					if err.Error() != "read |0: file already closed" {
						log.Println("Error reading daemon output:", err)
						d.Stop()
					}
					if isErrorOutput {
						log.Println("Stopping daemon error output handler, scanner returned an error")
					} else {
						log.Println("Stopping daemon normal output handler, scanner returned an error")
					}
					return
				}
			} // Otherwise we're still waiting for the process to start
		}
	}
}
