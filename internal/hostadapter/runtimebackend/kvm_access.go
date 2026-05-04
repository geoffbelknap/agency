package runtimebackend

import (
	"fmt"
	"os"
	"runtime"
	"strings"
)

const linuxKVMDevice = "/dev/kvm"

func CheckLinuxKVMAccess() error {
	if runtime.GOOS != "linux" {
		return nil
	}
	if strings.TrimSpace(os.Getenv("AGENCY_SKIP_KVM_ACCESS_CHECK")) != "" {
		return nil
	}
	f, err := os.OpenFile(linuxKVMDevice, os.O_RDWR, 0)
	if err == nil {
		_ = f.Close()
		return nil
	}
	if os.IsNotExist(err) {
		return fmt.Errorf("%s is not present", linuxKVMDevice)
	}
	if os.IsPermission(err) {
		return fmt.Errorf("%s is not readable and writable by the current user", linuxKVMDevice)
	}
	return fmt.Errorf("%s cannot be opened read/write: %w", linuxKVMDevice, err)
}

func LinuxKVMAccessFix() string {
	base := "Make user a member of the kvm group:\n  sudo usermod -aG kvm $USER"
	if linuxIsWSL() {
		return base + "\nThen run wsl.exe --shutdown from Windows"
	}
	return base + "\nThen start a new login session"
}

func linuxIsWSL() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return false
	}
	version := strings.ToLower(string(data))
	return strings.Contains(version, "microsoft") || strings.Contains(version, "wsl")
}
