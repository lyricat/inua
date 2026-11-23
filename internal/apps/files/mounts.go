package files

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// Mount is a mounted filesystem, from /proc/self/mountinfo.
type Mount struct {
	ID       int
	Point    string // where it is mounted
	Root     string // the part of the filesystem mounted there
	FSType   string
	Source   string // the device, or what the filesystem says it came from
	Device   uint64 // major:minor as one number (unix.Mkdev)
	ReadOnly bool
}

// ReadMounts reads the mount table.
func ReadMounts() ([]Mount, error) {
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseMountinfo(f), nil
}

// parseMountinfo reads mountinfo(5) lines:
//
//	36 35 98:0 /mnt1 /mnt2 rw,noatime master:1 - ext3 /dev/root rw,errors=continue
func parseMountinfo(r io.Reader) []Mount {
	var out []Mount
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		sep := -1
		for i, s := range f {
			if s == "-" {
				sep = i
				break
			}
		}
		if len(f) < 6 || sep < 6 || sep+2 >= len(f) {
			continue
		}
		var m Mount
		m.ID, _ = strconv.Atoi(f[0])
		if maj, min, ok := strings.Cut(f[2], ":"); ok {
			a, _ := strconv.ParseUint(maj, 10, 32)
			b, _ := strconv.ParseUint(min, 10, 32)
			m.Device = unix.Mkdev(uint32(a), uint32(b))
		}
		m.Root, m.Point = unescapeMount(f[3]), unescapeMount(f[4])
		m.ReadOnly = strings.HasPrefix(f[5], "ro")
		m.FSType, m.Source = f[sep+1], unescapeMount(f[sep+2])
		out = append(out, m)
	}
	return out
}

// unescapeMount undoes the kernel's octal escapes (\040 for a space).
func unescapeMount(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// pseudoFS are filesystems the user does not browse as devices.
var pseudoFS = map[string]bool{
	"proc": true, "sysfs": true, "devtmpfs": true, "devpts": true, "tmpfs": true,
	"cgroup": true, "cgroup2": true, "securityfs": true, "pstore": true, "bpf": true,
	"debugfs": true, "tracefs": true, "configfs": true, "fusectl": true, "mqueue": true,
	"hugetlbfs": true, "autofs": true, "binfmt_misc": true, "efivarfs": true,
	"squashfs": true, "overlay": true, "nsfs": true, "ramfs": true, "rpc_pipefs": true,
	"selinuxfs": true, "fuse.portal": true, "fuse.gvfsd-fuse": true, "fuse.snapfuse": true,
	"fuse.xdg-document-portal": true,
}

// UserMounts are the mounts the sidebar lists among the devices: real or
// network filesystems the user put somewhere to browse, not the system's
// own (/, /boot, /proc, snaps …). Mounts made by UDisks2 are left out, as
// the device list shows them already, unless includeMedia.
func UserMounts(ms []Mount, includeMedia bool) []Mount {
	var out []Mount
	seen := map[string]bool{}
	for _, m := range ms {
		if pseudoFS[m.FSType] || seen[m.Point] {
			continue
		}
		p := m.Point
		switch {
		case p == "/", p == "/boot", strings.HasPrefix(p, "/boot/"),
			strings.HasPrefix(p, "/proc"), strings.HasPrefix(p, "/sys"),
			strings.HasPrefix(p, "/dev"), strings.HasPrefix(p, "/run/") && !strings.HasPrefix(p, "/run/media/"),
			strings.HasPrefix(p, "/snap/"), strings.HasPrefix(p, "/var/"),
			strings.HasPrefix(p, "/usr"), strings.HasPrefix(p, "/nix"),
			strings.HasPrefix(p, "/tmp"), p == "/home" || p == "/root" || p == "/opt" || p == "/srv",
			// The system's own files and the bind mounts a sandbox (Orca's
			// fhsenv: /etc/X11, /etc/pam.d …, /.host-etc) makes into it.
			p == "/etc" || strings.HasPrefix(p, "/etc/") || p == "/.host-etc" || strings.HasPrefix(p, "/.host-etc/"):
			continue
		case !includeMedia && (strings.HasPrefix(p, "/media/") || strings.HasPrefix(p, "/run/media/")):
			continue
		}
		seen[p] = true
		out = append(out, m)
	}
	return out
}

// MountOf is the mount that holds path: the one with the longest mount
// point that is path or a parent of it.
func MountOf(ms []Mount, path string) (Mount, bool) {
	var best Mount
	found := false
	for _, m := range ms {
		if m.Point == path || m.Point == "/" || strings.HasPrefix(path, m.Point+"/") {
			if !found || len(m.Point) >= len(best.Point) {
				best, found = m, true
			}
		}
	}
	return best, found
}

// Space is a filesystem's size and what is free in it for the user.
type Space struct{ Total, Free uint64 }

// SpaceOf reads the space of the filesystem holding path.
func SpaceOf(path string) (Space, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return Space{}, err
	}
	bs := uint64(st.Bsize)
	return Space{Total: st.Blocks * bs, Free: st.Bavail * bs}, nil
}

// WatchMounts calls changed (on its own goroutine) after the mount table
// changes, until ctx ends: the kernel marks /proc/self/mountinfo with
// POLLPRI when something is mounted or unmounted.
func WatchMounts(ctx context.Context, changed func()) error {
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return err
	}
	var wake [2]int
	if err := unix.Pipe2(wake[:], unix.O_CLOEXEC|unix.O_NONBLOCK); err != nil {
		f.Close()
		return err
	}
	go func() {
		<-ctx.Done()
		unix.Write(wake[1], []byte{0})
	}()
	go func() {
		defer f.Close()
		defer unix.Close(wake[0])
		defer unix.Close(wake[1])
		fd := int32(f.Fd())
		for {
			fds := []unix.PollFd{{Fd: fd, Events: unix.POLLPRI}, {Fd: int32(wake[0]), Events: unix.POLLIN}}
			if _, err := unix.Poll(fds, -1); err != nil && !errors.Is(err, unix.EINTR) {
				return
			}
			if ctx.Err() != nil {
				return
			}
			if fds[0].Revents&(unix.POLLPRI|unix.POLLERR) != 0 {
				// Reading the table again clears the mark.
				f.Seek(0, io.SeekStart)
				io.Copy(io.Discard, f)
				changed()
			}
		}
	}()
	return nil
}
