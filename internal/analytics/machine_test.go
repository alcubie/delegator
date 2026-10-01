package analytics

import (
	"errors"
	"net"
	"slices"
	"strings"
	"testing"
)

func TestMachineIDNeverReadsBeforeConsent(t *testing.T) {
	no := false
	for _, consent := range []*bool{nil, &no} {
		for _, goos := range []string{"linux", "darwin", "windows", "unknown"} {
			if got := deriveMachineID(consent, goos, machineReaders{}); got != "" {
				t.Fatalf("%s nonconsenting machine ID = %q", goos, got)
			}
		}
		if got := MachineID(consent); got != "" {
			t.Fatalf("public nonconsenting machine ID = %q", got)
		}
	}
}

func TestMachineIDPlatformReadersNormalizeAndSeparateSources(t *testing.T) {
	yes := true
	seen := make(map[string]bool)
	// Fixed vectors pin the application key, source separator, normalization,
	// and HMAC-SHA256 output used by future reporting consumers.
	want := map[string]string{
		"linux":   "e64698de414101d26e9d207588887d6f4196dcca1f6419b946c81d6484e97aba",
		"darwin":  "9dfb7cfe17c9c7985beef9736a840970f8430ab5de78396bfb0d2590a57e7a8f",
		"windows": "64f6897759aaf1252c5494a72d72fa44fef6a2ba34acc898d4b856cfe5df4145",
	}
	for _, goos := range []string{"linux", "darwin", "windows"} {
		t.Run(goos, func(t *testing.T) {
			readers := machineReaders{
				readFile: func(path string) ([]byte, error) {
					if path != "/etc/machine-id" {
						t.Fatalf("unexpected path %q", path)
					}
					return []byte("  AABBCCDD11223344556677889900AABB\n"), nil
				},
				command: func(name string, args ...string) ([]byte, error) {
					switch goos {
					case "darwin":
						if name != "/usr/sbin/ioreg" || !slices.Equal(args, []string{"-rd1", "-c", "IOPlatformExpertDevice"}) {
							t.Fatalf("unexpected command %s %v", name, args)
						}
						return []byte("    \"IOPlatformUUID\" = \"AABBCCDD-1122-3344-5566-77889900AABB\"\n"), nil
					case "windows":
						if name != "reg.exe" || !slices.Equal(args, []string{"query", `HKLM\SOFTWARE\Microsoft\Cryptography`, "/v", "MachineGuid", "/reg:64"}) {
							t.Fatalf("unexpected command %s %v", name, args)
						}
						return []byte("HKEY_LOCAL_MACHINE\\SOFTWARE\\Microsoft\\Cryptography\r\n    MachineGuid    REG_SZ    aabbccdd-1122-3344-5566-77889900aabb\r\n"), nil
					default:
						t.Fatal("Linux must not execute a command")
						return nil, nil
					}
				},
				interfaces: func() ([]net.Interface, error) {
					t.Fatal("valid OS ID must win over MAC fallback")
					return nil, nil
				},
			}
			got := deriveMachineID(&yes, goos, readers)
			if got != want[goos] {
				t.Fatalf("machine ID = %q, want %q", got, want[goos])
			}
			if len(got) != 64 || strings.Contains(got, "aabbccdd11223344") || seen[got] {
				t.Fatalf("bad or non-separated hash: %q", got)
			}
			seen[got] = true
			if repeated := deriveMachineID(&yes, goos, readers); got != repeated {
				t.Fatal("unstable OS hash")
			}
		})
	}
}

func TestMachineIDLinuxSecondaryFile(t *testing.T) {
	yes := true
	var paths []string
	got := deriveMachineID(&yes, "linux", machineReaders{
		readFile: func(path string) ([]byte, error) {
			paths = append(paths, path)
			if path == "/etc/machine-id" {
				return []byte("uninitialized\n"), nil
			}
			return []byte("aabbccdd11223344556677889900aabb"), nil
		},
	})
	if got == "" || !slices.Equal(paths, []string{"/etc/machine-id", "/var/lib/dbus/machine-id"}) {
		t.Fatalf("secondary file lookup: %q, %v", got, paths)
	}
}

func TestMachineIDMACFallbackIsDeterministicAndFiltersUnsuitableAddresses(t *testing.T) {
	yes := true
	interfaces := []net.Interface{
		{HardwareAddr: net.HardwareAddr{0x02, 0, 0, 0, 0, 2}},
		{HardwareAddr: net.HardwareAddr{0x02, 0, 0, 0, 0, 1}},
		{HardwareAddr: net.HardwareAddr{0, 0, 0, 0, 0, 1}, Flags: net.FlagLoopback},
		{HardwareAddr: net.HardwareAddr{0x01, 0, 0, 0, 0, 1}},
		{HardwareAddr: net.HardwareAddr{0, 0, 0, 0, 0, 0}},
		{HardwareAddr: net.HardwareAddr{0}},
		{},
	}
	readers := machineReaders{
		readFile:   func(string) ([]byte, error) { return nil, errors.New("unavailable") },
		command:    func(string, ...string) ([]byte, error) { return nil, errors.New("unavailable") },
		interfaces: func() ([]net.Interface, error) { return interfaces, nil },
	}
	first := deriveMachineID(&yes, "linux", readers)
	if len(first) != 64 {
		t.Fatalf("MAC fallback = %q", first)
	}
	slices.Reverse(interfaces)
	for _, goos := range []string{"linux", "darwin", "windows", "unknown"} {
		if got := deriveMachineID(&yes, goos, readers); got != first {
			t.Fatalf("enumeration or OS changed fallback: %s != %s", got, first)
		}
	}
	interfaces = []net.Interface{{HardwareAddr: net.HardwareAddr{0x02, 0, 0, 0, 0, 1}}}
	if got := deriveMachineID(&yes, "linux", readers); got != first {
		t.Fatalf("did not choose the smallest usable MAC: %q", got)
	}
	interfaces = []net.Interface{
		{HardwareAddr: net.HardwareAddr{0x02, 0, 0, 0, 0, 1}, Flags: net.FlagLoopback},
		{HardwareAddr: net.HardwareAddr{0x01, 0, 0, 0, 0, 1}},
		{HardwareAddr: net.HardwareAddr{0, 0, 0, 0, 0, 0}},
		{HardwareAddr: net.HardwareAddr{0x02}},
		{},
	}
	if got := deriveMachineID(&yes, "linux", readers); got != "" {
		t.Fatalf("invented ID from unusable MACs: %q", got)
	}
	interfaces = nil
	if got := deriveMachineID(&yes, "linux", readers); got != "" {
		t.Fatalf("invented ID without inputs: %q", got)
	}
	readers.interfaces = func() ([]net.Interface, error) { return nil, errors.New("unavailable") }
	if got := deriveMachineID(&yes, "linux", readers); got != "" {
		t.Fatalf("invented ID after error: %q", got)
	}
}

func TestNormalizeMachineID(t *testing.T) {
	const want = "aabbccdd11223344556677889900aabb"
	for _, raw := range []string{
		want,
		"  AABBCCDD11223344556677889900AABB\n",
		"\tAABBCCDD-1122-3344-5566-77889900AABB\r\n",
	} {
		if got := normalizeMachineID(raw); got != want {
			t.Errorf("normalizeMachineID(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestInvalidOSIdentifiersFallBack(t *testing.T) {
	yes := true
	for _, raw := range []string{"", "uninitialized", strings.Repeat("0", 32), strings.Repeat("f", 32), strings.Repeat("g", 32)} {
		for _, goos := range []string{"linux", "darwin", "windows"} {
			readers := machineReaders{
				readFile: func(string) ([]byte, error) { return []byte(raw), nil },
				command: func(string, ...string) ([]byte, error) {
					return []byte(`"IOPlatformUUID" = "` + raw + "\"\nMachineGuid REG_SZ " + raw), nil
				},
				interfaces: func() ([]net.Interface, error) { return nil, nil },
			}
			if got := deriveMachineID(&yes, goos, readers); got != "" {
				t.Fatalf("%s accepted invalid input %q: %q", goos, raw, got)
			}
		}
	}
}
