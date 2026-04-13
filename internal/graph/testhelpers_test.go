package graph

import "os"

func openFile(p string) (*os.File, error) { return os.Open(p) }
