package store

import (
	"fmt"
	"syscall"
)

// Filesystem magic numbers from statfs(2).
var linuxNetworkFS = map[int64]string{
	0x6969:     "nfs",
	0x517B:     "smb",
	0xFF534D42: "cifs",
	0xFE534D42: "smb2",
	0x65735546: "fuse",
	0x5346414F: "afs",
	0x73757245: "coda",
	0x01021997: "9p",
	0x00C36400: "ceph",
	0x01161970: "gfs2",
	0x7461636F: "ocfs2",
	0x47504653: "gpfs",
	0x0BD00BD0: "lustre",
}

func fsType(dir string) (string, bool, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return "", false, err
	}
	t := int64(st.Type) & 0xFFFFFFFF
	if name, ok := linuxNetworkFS[t]; ok {
		return name, true, nil
	}
	return fmt.Sprintf("0x%x", t), false, nil
}
