package models

type Checksum string

func (c Checksum) String() string { return string(c) }