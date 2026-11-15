package store

import "syscall"

var darwinNetworkFS = map[string]bool{
	"nfs": true, "smbfs": true, "afpfs": true, "webdav": true, "cifs": true,
	"ftp": true, "macfuse": true, "osxfuse": true, "fusefs": true,
}

func fsType(dir string) (string, bool, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return "", false, err
	}
	b := make([]byte, 0, len(st.Fstypename))
	for _, c := range st.Fstypename {
		if c == 0 {
			break
		}
		b = append(b, byte(c))
	}
	name := string(b)
	const mntLocal = 0x00001000 // MNT_LOCAL in <sys/mount.h>
	return name, darwinNetworkFS[name] || st.Flags&mntLocal == 0, nil
}
