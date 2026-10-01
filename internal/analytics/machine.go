// Package analytics provides internal, consent-gated reporting primitives.
package analytics

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"time"
)

// This public application key separates this hash from other applications. It
// is not a secret or credential. Raw OS identifiers and MACs never leave here.
const machineKey = "delegator.analytics.machine-id.v1"

type machineReaders struct {
	readFile   func(string) ([]byte, error)
	command    func(string, ...string) ([]byte, error)
	interfaces func() ([]net.Interface, error)
}

// MachineID returns an optional application-specific hash, derived only after
// explicit consent. It writes no state and does not detect copied databases.
// Kept as the identity entry point for the aggregate builder and sender.
func MachineID(consent *bool) string {
	return deriveMachineID(consent, runtime.GOOS, machineReaders{
		readFile: os.ReadFile,
		command: func(name string, args ...string) ([]byte, error) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			return exec.CommandContext(ctx, name, args...).Output()
		},
		interfaces: net.Interfaces,
	})
}

func deriveMachineID(consent *bool, goos string, readers machineReaders) string {
	if consent == nil || !*consent {
		return ""
	}
	source, identifier := osMachineID(goos, readers)
	if identifier == "" {
		interfaces, err := readers.interfaces()
		if err != nil {
			return ""
		}
		var candidates []string
		for _, iface := range interfaces {
			mac := iface.HardwareAddr
			if iface.Flags&net.FlagLoopback != 0 || (len(mac) != 6 && len(mac) != 8) || mac[0]&1 != 0 {
				continue
			}
			if !slices.ContainsFunc(mac, func(b byte) bool { return b != 0 }) {
				continue
			}
			candidates = append(candidates, hex.EncodeToString(mac))
		}
		if len(candidates) == 0 {
			return ""
		}
		slices.Sort(candidates)
		source, identifier = "mac", candidates[0]
	}
	hash := hmac.New(sha256.New, []byte(machineKey))
	hash.Write([]byte(source + "\x00" + identifier))
	return hex.EncodeToString(hash.Sum(nil))
}

func osMachineID(goos string, readers machineReaders) (string, string) {
	switch goos {
	case "linux":
		for _, path := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
			if data, err := readers.readFile(path); err == nil {
				if id := normalizeMachineID(string(data)); id != "" {
					return "linux-machine-id", id
				}
			}
		}
	case "darwin":
		data, err := readers.command("/usr/sbin/ioreg", "-rd1", "-c", "IOPlatformExpertDevice")
		if err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				key, value, ok := strings.Cut(line, "=")
				if ok && strings.TrimSpace(key) == `"IOPlatformUUID"` {
					return "darwin-platform-uuid", normalizeMachineID(strings.Trim(strings.TrimSpace(value), `"`))
				}
			}
		}
	case "windows":
		data, err := readers.command("reg.exe", "query", `HKLM\SOFTWARE\Microsoft\Cryptography`, "/v", "MachineGuid", "/reg:64")
		if err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				fields := strings.Fields(line)
				if len(fields) == 3 && fields[0] == "MachineGuid" && fields[1] == "REG_SZ" {
					return "windows-machine-guid", normalizeMachineID(fields[2])
				}
			}
		}
	}
	return "", ""
}

func normalizeMachineID(raw string) string {
	id := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(raw)), "-", "")
	if len(id) != 32 || id == strings.Repeat("0", 32) || id == strings.Repeat("f", 32) {
		return ""
	}
	if _, err := hex.DecodeString(id); err != nil {
		return ""
	}
	return id
}
