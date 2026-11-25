//go:build darwin

package proc

import (
	"bytes"
	"errors"

	"golang.org/x/sys/unix"
)

// O macOS não tem /proc: comm e pai vêm do sysctl kern.proc.pid, executável
// e argv[0] do kern.procargs2 (só legível para processos do mesmo usuário) e
// a existência do processo de kill(pid, 0).
func init() {
	native = &nativeOps{names: darwinNames, parent: darwinParent, alive: darwinAlive}
}

func kinfo(pid int) (*unix.KinfoProc, error) {
	k, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return nil, err
	}
	if int(k.Proc.P_pid) != pid {
		return nil, errors.New("processo não encontrado")
	}
	return k, nil
}

func darwinNames(pid int) []string {
	k, err := kinfo(pid)
	if err != nil {
		return nil
	}
	comm := k.Proc.P_comm[:]
	if i := bytes.IndexByte(comm, 0); i >= 0 {
		comm = comm[:i]
	}
	var exe, argv0 string
	if b, err := unix.SysctlRaw("kern.procargs2", pid); err == nil {
		exe, argv0 = parseProcArgs(b)
	}
	return baseNames(string(comm), exe, argv0)
}

func darwinParent(pid int) (int, error) {
	k, err := kinfo(pid)
	if err != nil {
		return 0, err
	}
	return int(k.Eproc.Ppid), nil
}

func darwinAlive(pid int) bool {
	err := unix.Kill(pid, 0)
	return err == nil || errors.Is(err, unix.EPERM)
}
