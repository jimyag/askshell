package remote

import (
	_ "embed"
)

//go:embed helper_linux_amd64
var helperLinuxAMD64 []byte

//go:embed helper_linux_arm64
var helperLinuxARM64 []byte

// helperBinary picks the prebuilt askshell-remote binary for the detected
// remote platform. The remote is probed with uname before upload.
func helperBinary(platform remotePlatform) ([]byte, bool) {
	switch {
	case platform.os == "linux" && platform.arch == "x86_64":
		return helperLinuxAMD64, true
	case platform.os == "linux" && (platform.arch == "aarch64" || platform.arch == "arm64"):
		return helperLinuxARM64, true
	default:
		return nil, false
	}
}
