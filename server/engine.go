package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"time"
)

type EngineProcess struct {
	SockPath    string
	DataDirPath string

	cmd *exec.Cmd
}

func (e *EngineProcess) Spawn() error {
	e.cmd = exec.Command("engine-server", "-sock", e.SockPath, "-data", e.DataDirPath)
	e.cmd.Stdout = os.Stdout
	e.cmd.Stderr = os.Stderr
	if err := e.cmd.Start(); err != nil {
		return fmt.Errorf("spawn engine-server: %w", err)
	}
	return e.waitForSocket()
}

func (e *EngineProcess) waitForSocket() error {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.Dial("unix", e.SockPath)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("engine-server did not come up at %s", e.SockPath)
}

func (e *EngineProcess) Destroy() error {
	if e.cmd == nil || e.cmd.Process == nil {
		return nil
	}
	if err := e.cmd.Process.Kill(); err != nil {
		return err
	}
	e.cmd.Wait()
	e.cmd = nil
	os.Remove(e.SockPath)
	return nil
}

func (e *EngineProcess) DestroyCompletely() error {
	if err := e.Destroy(); err != nil {
		return err
	}
	return os.RemoveAll(e.DataDirPath)
}

func (e *EngineProcess) Respawn() error {
	if err := e.Destroy(); err != nil {
		return err
	}
	return e.Spawn()
}
