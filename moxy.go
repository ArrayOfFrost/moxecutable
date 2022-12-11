package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Moxecutable struct {
	Pid  int            `json:"pid"`
	Ppid int            `json:"ppid"`
	Cwd  string         `json:"cwd"`
	Env  []string       `json:"env"`
	Args []string       `json:"args"`
	Log  chan string    `json:"-"`
	Exit bool           `json:"-"`
	Dir  string         `json:"-"`
	wg   sync.WaitGroup `json:"-"`
}

func (mox *Moxecutable) openFifo(fileName string) (io.ReadCloser, io.WriteCloser) {
	pathName := filepath.Join(mox.Dir, fileName)
	err := syscall.Mkfifo(pathName, 0755)
	if err != nil {
		mox.Log <- err.Error()
		return nil, nil
	}
	reading, err := os.OpenFile(pathName, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		mox.Log <- err.Error()
		return nil, nil
	}
	writing, err := os.OpenFile(pathName, os.O_WRONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		mox.Log <- err.Error()
		return nil, nil
	}
	return reading, writing
}

func (mox *Moxecutable) forwardingLoop(from io.Reader, to io.Writer) {
	for {
		io.Copy(to, from)
		time.Sleep(100 * time.Millisecond)
	}
}

func (mox *Moxecutable) writeContext() {
	// Write context to file
	mox.Log <- "Writing context file"
	contextFile, err := os.Create(filepath.Join(mox.Dir, "context.json"))
	if err != nil {
		mox.Log <- err.Error()
		return
	}
	defer contextFile.Close()
	encoder := json.NewEncoder(contextFile)
	encoder.SetIndent("", "  ")
	err = encoder.Encode(mox)
	if err != nil {
		mox.Log <- err.Error()
	}
}

func newMoxecutable() *Moxecutable {
	var err error
	mox := &Moxecutable{}
	mox.Pid = os.Getpid()
	mox.Env = os.Environ()
	mox.Ppid = os.Getppid()
	mox.Args = os.Args
	mox.wg = sync.WaitGroup{}
	mox.Log = make(chan string, 5)
	mox.Cwd, err = os.Getwd()
	if err != nil {
		mox.Log <- err.Error()
	}
	homedir, err := os.UserHomeDir()
	if err != nil {
		mox.Log <- err.Error()
	}
	mox.Dir = filepath.Join(homedir, "moxecutable", mox.Args[0], strconv.Itoa(mox.Pid))
	err = os.MkdirAll(mox.Dir, 0755)
	if err != nil {
		mox.Log <- err.Error()
	}
	go mox.handleLogs()
	go mox.handleInput()
	mox.writeContext()
	return mox
}

func (mox *Moxecutable) handleLogs() {
	// Must be called after mox.Dir is assigned
	logFile, err := os.Create(filepath.Join(mox.Dir, "log.txt"))
	if err != nil {
		panic("Unable to create logs file")
	}
	defer logFile.Close()
	for message := range mox.Log {
		logFile.WriteString(message + "\n")
	}
}

func (mox *Moxecutable) handleInput() {
	stdin, err := os.Create(filepath.Join(mox.Dir, "stdin"))
	if err != nil {
		panic("Unable to create stdin file")
	}
	for {
		io.Copy(stdin, os.Stdin)
		time.Sleep(100 * time.Millisecond)
	}
}

func doWork() (exitCode int) {
	mox := newMoxecutable()
	rOut, wOut := mox.openFifo("stdout")
	defer wOut.Close()
	defer rOut.Close()
	go mox.forwardingLoop(rOut, os.Stdout)
	rErr, wErr := mox.openFifo("stderr")
	defer wErr.Close()
	defer rErr.Close()
	go mox.forwardingLoop(rErr, os.Stderr)

	mox.Log <- "Waiting for exit"
	r, _ := mox.openFifo("mox_exit")
	defer close(mox.Log)

	for {
		b := make([]byte, 8)
		n, _ := r.Read(b)
		if n > 0 {
			strVal := strings.Fields(string(b[:n]))[0]
			exitCode, _ = strconv.Atoi(strVal)
			mox.Log <- fmt.Sprintf("Got strVal: %s, returning exit code %d", strVal, exitCode)
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	return
}

func main() {
	os.Exit(doWork())
}
