package main

// ssh_fuzz_test.go -- v0.61 architecture block 5: fuzz target
// for SSH key parsing. Private keys are user-supplied files
// dropped into Data/ssh/keys and parsed at connect time
// (dialSSH); authorized-key strings arrive from servers. The
// parsers (golang.org/x/crypto/ssh) must reject garbage with
// errors, never panics.

import (
	"testing"

	"golang.org/x/crypto/ssh"
)

func FuzzSSHKeyParse(f *testing.F) {
	f.Add([]byte("-----BEGIN OPENSSH PRIVATE KEY-----\nAAAA\n-----END OPENSSH PRIVATE KEY-----\n"))
	f.Add([]byte("-----BEGIN RSA PRIVATE KEY-----\nAAAA\n-----END RSA PRIVATE KEY-----\n"))
	f.Add([]byte("-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----\n"))
	f.Add([]byte("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl user@host"))
	f.Add([]byte("ssh-rsa AAAAB3NzaC1yc2E= comment"))
	f.Add([]byte(""))
	f.Add([]byte("not a key"))
	f.Add([]byte{0x00, 0x01, 0x02, 0xff})
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = ssh.ParsePrivateKey(data)
		_, _ = ssh.ParsePrivateKeyWithPassphrase(data, []byte("passphrase"))
		_, _, _, _, _ = ssh.ParseAuthorizedKey(data)
	})
}
