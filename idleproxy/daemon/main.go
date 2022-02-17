package daemon

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log"
	"os/exec"
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
	Status Status
	cmd    *exec.Cmd
}

// Return a new daemon
func NewDaemon(cwd, cmd string) *Daemon {
	return &Daemon{
		Cwd:    cwd,
		Cmd:    cmd,
		Status: Stopped,
	}
}

func (d *Daemon) Start() error {
	if d.Status != Stopped {
		return errors.New("unable to start daemon, already running")
	}

	d.Status = Starting

	// cmd = exec.Command(d.Cmd)
	d.cmd = exec.Command("/bin/bash", "-c", d.Cmd)
	d.cmd.Dir = d.Cwd

	cmdReader, cmdReaderErr := d.cmd.StdoutPipe()
	if cmdReaderErr != nil {
		log.Printf("Error getting daemon stdout pipe: %s", cmdReaderErr)
		d.Status = Stopped
		return cmdReaderErr
	}
	go handleOutput(d, cmdReader)

	if err := d.cmd.Start(); err != nil {
		log.Println("Error start daemon:", err)
		d.Status = Stopped
		return err
	}

	d.Status = Running
	return nil
}

func (d *Daemon) Stop() error {
	if d.Status == Stopped {
		return errors.New("unable to stop daemon, already stopped")
	}

	// TODO: Somehow gracefully shutdown or kill the command/process
	// if err := d.cmd.Process.Signal(os.Interrupt); err != nil {
	if err := d.cmd.Process.Kill(); err != nil {
		log.Println("Error stopping daemon:", err)
		return err
	}
	if err := d.cmd.Wait(); err != nil {
		// Ignore the error if the process was killed
		if err.Error() != "signal: killed" {
			log.Println("Error waiting for daemon to stop:", err)
			return err
		}
	}

	d.Status = Stopped
	return nil
}

func handleOutput(d *Daemon, cmdReader io.ReadCloser) {
	// defer d.Stop()

	scanner := bufio.NewScanner(cmdReader)

	for {
		select {
		case <-d.Context.Done():
			log.Println("Context done, stopping daemon output handler")
			return
		default:
			for scanner.Scan() {
				log.Println(scanner.Text())
			}
			if err := scanner.Err(); err != nil {
				log.Println("Error reading daemon output:", err)
			}
		}
	}
}
