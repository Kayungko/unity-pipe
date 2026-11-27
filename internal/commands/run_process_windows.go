package commands

import (
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"
)

var (
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	createEditorJob    = kernel32.NewProc("CreateJobObjectW")
	configureEditorJob = kernel32.NewProc("SetInformationJobObject")
	assignEditorJob    = kernel32.NewProc("AssignProcessToJobObject")
)

type editorJobBasicLimits struct {
	ProcessTime, JobTime int64
	Flags                uint32
	Min, Max             uintptr
	ActiveProcessLimit   uint32
	Affinity             uintptr
	Priority, Scheduling uint32
}

type editorJobLimits struct {
	Basic                    editorJobBasicLimits
	IO                       [6]uint64
	ProcessMemory, JobMemory uintptr
	PeakProcess, PeakJob     uintptr
}

func startScopedEditor(cmd *exec.Cmd) (func() error, error) {
	handle, _, callErr := createEditorJob.Call(0, 0)
	if handle == 0 {
		return nil, fmt.Errorf("create Editor job object: %w", callErr)
	}
	closeJob := func() error { return syscall.CloseHandle(syscall.Handle(handle)) }
	const extendedLimitsClass = 9
	const killOnJobClose = 0x2000
	limits := editorJobLimits{Basic: editorJobBasicLimits{Flags: killOnJobClose}}
	ok, _, callErr := configureEditorJob.Call(handle, extendedLimitsClass, uintptr(unsafe.Pointer(&limits)), unsafe.Sizeof(limits))
	if ok == 0 {
		_ = closeJob()
		return nil, fmt.Errorf("configure Editor job object: %w", callErr)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
	if err := cmd.Start(); err != nil {
		_ = closeJob()
		return nil, err
	}
	const processSetQuota = 0x0100
	process, err := syscall.OpenProcess(syscall.PROCESS_TERMINATE|processSetQuota, false, uint32(cmd.Process.Pid))
	if err == nil {
		ok, _, callErr = assignEditorJob.Call(handle, uintptr(process))
		_ = syscall.CloseHandle(process)
		if ok == 0 {
			err = callErr
		}
	}
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = closeJob()
		return nil, fmt.Errorf("assign temporary Editor to its job object: %w", err)
	}
	return closeJob, nil
}

func processAlive(pid int) bool {
	const processQueryLimitedInformation = 0x1000
	const stillActive = 259
	handle, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		return err == syscall.ERROR_ACCESS_DENIED
	}
	defer syscall.CloseHandle(handle)
	var code uint32
	return syscall.GetExitCodeProcess(handle, &code) == nil && code == stillActive
}
