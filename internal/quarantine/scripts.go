package quarantine

import _ "embed"

//go:embed quarantine.swift
var QuarantineScript []byte

//go:embed trash.swift
var TrashScript []byte

//go:embed copy-xattrs.swift
var CopyXattrsScript []byte
