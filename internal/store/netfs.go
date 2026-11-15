package store

import "fmt"

// checkLocalFS refuses a store directory on a network filesystem. SQLite's
// WAL mode keeps its index in shared memory beside the database, which only
// works when every process sees the same local file; over NFS, SMB and
// similar filesystems that locking is unreliable and the store can corrupt.
func checkLocalFS(dir string) error {
	name, network, err := fsType(dir)
	if err != nil {
		return fmt.Errorf("checking the filesystem of %s: %w", dir, err)
	}
	if network {
		return fmt.Errorf("%s is on a network filesystem (%s). The store uses SQLite's WAL mode, "+
			"which needs a local filesystem: its shared-memory locking does not work over the network. "+
			"Set AGENT_DESK_HOME to a local directory, and use `agent-desk merge` to combine stores "+
			"of different machines", dir, name)
	}
	return nil
}
