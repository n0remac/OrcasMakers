package webrtc

import "bytes"

func newByteReadSeeker(contents []byte) *bytes.Reader { return bytes.NewReader(contents) }
