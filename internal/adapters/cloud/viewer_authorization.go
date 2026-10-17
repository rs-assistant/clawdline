package cloud

import (
	"errors"

	domain "github.com/sainteye/clawdline/internal/domain/cloud"
)

// Whether this machine holds a usable viewer authorization is decided here
// once. The daemon's local viewer line and the `cloud viewer` commands each say
// it in their own words, so the answer is typed and each caller maps it.
var (
	// ErrViewerIdentityAbsent: no viewer identity has been saved.
	ErrViewerIdentityAbsent = errors.New("viewer identity is absent")
	// ErrViewerIdentityUnreadable: the identity file could not be read or
	// used. The error that matches it reads as its cause.
	ErrViewerIdentityUnreadable = errors.New("viewer identity is unreadable")
	// ErrViewerEnvironmentMismatch: the identity was issued by another Cloud
	// API than the one this machine is set to.
	ErrViewerEnvironmentMismatch = errors.New("viewer identity belongs to another Cloud API")
	// ErrViewerKeyMissing: the viewer signing key is not on disk.
	ErrViewerKeyMissing = errors.New("viewer signing key is missing")
	// ErrViewerKeyUnreadable: the signing key could not be read. The error
	// that matches it reads as its cause.
	ErrViewerKeyUnreadable = errors.New("viewer signing key is unreadable")
)

// ViewerKeys is the part of the key files a viewer authorization reads.
type ViewerKeys interface {
	Dir() string
	ViewerKey() (domain.DeviceKey, bool, error)
}

// LoadViewerIdentity is the saved viewer identity, when it is usable against
// apiBase. The error is one of the ErrViewerIdentity* or
// ErrViewerEnvironmentMismatch values, the unreadable one wrapping its cause.
func LoadViewerIdentity(dir, apiBase string) (ViewerIdentity, error) {
	identity, found, err := NewViewerIdentityStore(dir).Load()
	switch {
	case err != nil:
		return ViewerIdentity{}, viewerUnreadable{kind: ErrViewerIdentityUnreadable, cause: err}
	case !found:
		return ViewerIdentity{}, ErrViewerIdentityAbsent
	case identity.APIBase != apiBase:
		return ViewerIdentity{}, ErrViewerEnvironmentMismatch
	}
	return identity, nil
}

// LoadViewerAuthorization is the identity and the signing key a viewer line
// needs, or the first reason there is none: the identity is checked before the
// key, because a key is no use to an identity this API did not issue.
func LoadViewerAuthorization(keys ViewerKeys, apiBase string) (ViewerIdentity, domain.DeviceKey, error) {
	identity, err := LoadViewerIdentity(keys.Dir(), apiBase)
	if err != nil {
		return ViewerIdentity{}, domain.DeviceKey{}, err
	}
	key, found, err := keys.ViewerKey()
	if err != nil {
		return ViewerIdentity{}, domain.DeviceKey{}, viewerUnreadable{kind: ErrViewerKeyUnreadable, cause: err}
	}
	if !found {
		return ViewerIdentity{}, domain.DeviceKey{}, ErrViewerKeyMissing
	}
	return identity, key, nil
}

// viewerUnreadable is one of the unreadable kinds, worded as its cause so a
// caller that prints it says what the file system said.
type viewerUnreadable struct{ kind, cause error }

func (e viewerUnreadable) Error() string        { return e.cause.Error() }
func (e viewerUnreadable) Unwrap() error        { return e.cause }
func (e viewerUnreadable) Is(target error) bool { return target == e.kind }
