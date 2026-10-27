package jobs

import "syscall"

type statfs struct{ free int64 }

func statFS(dir string, s *statfs) error {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return err
	}
	s.free = int64(st.Bavail) * st.Bsize
	return nil
}
