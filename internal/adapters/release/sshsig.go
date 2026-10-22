package release

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha512"
	"encoding/base64"
	"encoding/binary"
	"strings"
)

// The same Ed25519 signature over manifest.json, in OpenSSH's SSHSIG format
// (manifest.sshsig), so install.sh can check a release with `ssh-keygen -Y
// verify`, which every macOS and most Linux machines have, before it runs
// anything it downloaded. A daemon still trusts only manifest.sig.json
// (Open); this is the installer's copy of the same promise.
//
// The format is OpenSSH's PROTOCOL.sshsig: the key signs
// "SSHSIG" || string(namespace) || string("") || string("sha512") ||
// string(SHA-512(message)), and the armored file carries the public key,
// the namespace and that signature.
const (
	// SSHNamespace is the namespace every release's SSH signature is made
	// in; install.sh verifies with the same -n.
	SSHNamespace = "clawdline-release"
	// SSHSignatureFile is the release asset that holds it.
	SSHSignatureFile = "manifest.sshsig"
	sshKeyType       = "ssh-ed25519"
	sshHash          = "sha512"
	sshArmorBegin    = "-----BEGIN SSH SIGNATURE-----"
	sshArmorEnd      = "-----END SSH SIGNATURE-----"
)

func sshString(b *bytes.Buffer, s []byte) {
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(s)))
	b.Write(n[:])
	b.Write(s)
}

func sshWire(fields ...[]byte) []byte {
	var b bytes.Buffer
	for _, f := range fields {
		sshString(&b, f)
	}
	return b.Bytes()
}

// sshSigned is what the key signs for message.
func sshSigned(message []byte) []byte {
	sum := sha512.Sum512(message)
	var b bytes.Buffer
	b.WriteString("SSHSIG")
	sshString(&b, []byte(SSHNamespace))
	sshString(&b, nil)
	sshString(&b, []byte(sshHash))
	sshString(&b, sum[:])
	return b.Bytes()
}

// SSHPublicKey is pub as an authorized_keys / allowed_signers key:
// "ssh-ed25519 AAAA…".
func SSHPublicKey(pub ed25519.PublicKey) string {
	return sshKeyType + " " + base64.StdEncoding.EncodeToString(sshWire([]byte(sshKeyType), pub))
}

// SignSSH is the armored SSHSIG signature of message by priv.
func SignSSH(message []byte, priv ed25519.PrivateKey) []byte {
	pub := priv.Public().(ed25519.PublicKey)
	sig := ed25519.Sign(priv, sshSigned(message))
	var blob bytes.Buffer
	blob.WriteString("SSHSIG")
	blob.Write([]byte{0, 0, 0, 1})
	sshString(&blob, sshWire([]byte(sshKeyType), pub))
	sshString(&blob, []byte(SSHNamespace))
	sshString(&blob, nil)
	sshString(&blob, []byte(sshHash))
	sshString(&blob, sshWire([]byte(sshKeyType), sig))
	enc := base64.StdEncoding.EncodeToString(blob.Bytes())
	var out strings.Builder
	out.WriteString(sshArmorBegin + "\n")
	for len(enc) > 70 {
		out.WriteString(enc[:70] + "\n")
		enc = enc[70:]
	}
	out.WriteString(enc + "\n" + sshArmorEnd + "\n")
	return []byte(out.String())
}

// sshReader reads SSH wire strings off a byte slice.
type sshReader struct {
	b   []byte
	bad bool
}

func (r *sshReader) str() []byte {
	if r.bad || len(r.b) < 4 {
		r.bad = true
		return nil
	}
	n := binary.BigEndian.Uint32(r.b)
	if uint64(n) > uint64(len(r.b)-4) {
		r.bad = true
		return nil
	}
	s := r.b[4 : 4+n]
	r.b = r.b[4+n:]
	return s
}

// VerifySSH checks that armored is an SSHSIG signature over message, in
// SSHNamespace, by one of keys. It is how `tools/release verify` proves the
// installer's copy of the signature before a release is drafted.
func VerifySSH(message, armored []byte, keys []ed25519.PublicKey) error {
	if len(keys) == 0 {
		return refuse(CodeNoTrustedKey, "no release key to verify %s with", SSHSignatureFile)
	}
	text := strings.TrimSpace(string(armored))
	body, ok := strings.CutPrefix(text, sshArmorBegin)
	if ok {
		body, ok = strings.CutSuffix(body, sshArmorEnd)
	}
	if !ok {
		return refuse(CodeSignatureInvalid, "%s is not an armored SSH signature", SSHSignatureFile)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(body), ""))
	if err != nil {
		return refuse(CodeSignatureInvalid, "%s is not base64: %v", SSHSignatureFile, err)
	}
	if !bytes.HasPrefix(raw, []byte("SSHSIG\x00\x00\x00\x01")) {
		return refuse(CodeSignatureInvalid, "%s is not an SSHSIG version 1 signature", SSHSignatureFile)
	}
	r := &sshReader{b: raw[10:]}
	pubWire, ns, _, hash, sigWire := r.str(), r.str(), r.str(), r.str(), r.str()
	if r.bad || len(r.b) != 0 {
		return refuse(CodeSignatureInvalid, "%s is truncated or has trailing bytes", SSHSignatureFile)
	}
	if string(ns) != SSHNamespace || string(hash) != sshHash {
		return refuse(CodeSignatureInvalid, "%s is in namespace %q with %q, not %q with %q", SSHSignatureFile, ns, hash, SSHNamespace, sshHash)
	}
	pr := &sshReader{b: pubWire}
	ktype, pub := pr.str(), pr.str()
	sr := &sshReader{b: sigWire}
	stype, sig := sr.str(), sr.str()
	if pr.bad || sr.bad || string(ktype) != sshKeyType || string(stype) != sshKeyType || len(pub) != ed25519.PublicKeySize {
		return refuse(CodeSignatureInvalid, "%s is not an Ed25519 signature", SSHSignatureFile)
	}
	for _, k := range keys {
		if bytes.Equal(k, pub) {
			if ed25519.Verify(k, sshSigned(message), sig) {
				return nil
			}
			return refuse(CodeSignatureInvalid, "%s does not match the manifest", SSHSignatureFile)
		}
	}
	return refuse(CodeSignatureInvalid, "%s is by key %s, which is not trusted", SSHSignatureFile, KeyID(pub))
}
