package web

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"math/big"
	"testing"

	"github.com/fxamacker/cbor/v2"
	"github.com/go-webauthn/webauthn/protocol"
)

// softAuthn is a software authenticator: it holds a P-256 key and produces
// WebAuthn registration and assertion responses the server can verify.
type softAuthn struct {
	priv    *ecdsa.PrivateKey
	credID  []byte
	cose    []byte
	user    []byte
	rpID    string
	counter uint32
}

func newSoftAuthn(t *testing.T) *softAuthn {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id := make([]byte, 32)
	if _, err := rand.Read(id); err != nil {
		t.Fatal(err)
	}
	return &softAuthn{priv: priv, credID: id, cose: canonicalCOSE(t, &priv.PublicKey)}
}

type coseEC2 struct {
	Kty int64  `cbor:"1,keyasint"`
	Alg int64  `cbor:"3,keyasint"`
	Crv int64  `cbor:"-1,keyasint"`
	X   []byte `cbor:"-2,keyasint"`
	Y   []byte `cbor:"-3,keyasint"`
}

func canonicalCOSE(t *testing.T, pub *ecdsa.PublicKey) []byte {
	t.Helper()
	em, err := cbor.CTAP2EncOptions().EncMode()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := em.Marshal(coseEC2{
		Kty: 2,
		Alg: -7,
		Crv: 1,
		X:   padCoord(pub.X),
		Y:   padCoord(pub.Y),
	})
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := cbor.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	out, err := em.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func padCoord(n *big.Int) []byte {
	out := make([]byte, 32)
	n.FillBytes(out)
	return out
}

func (a *softAuthn) authData(counter uint32, attested bool) []byte {
	sum := sha256.Sum256([]byte(a.rpID))
	flags := protocol.FlagUserPresent | protocol.FlagUserVerified
	buf := append([]byte{}, sum[:]...)
	if attested {
		flags |= protocol.FlagAttestedCredentialData
	}
	buf = append(buf, byte(flags))
	var count [4]byte
	binary.BigEndian.PutUint32(count[:], counter)
	buf = append(buf, count[:]...)
	if !attested {
		return buf
	}
	buf = append(buf, make([]byte, 16)...)
	var idLen [2]byte
	binary.BigEndian.PutUint16(idLen[:], uint16(len(a.credID)))
	buf = append(buf, idLen[:]...)
	buf = append(buf, a.credID...)
	buf = append(buf, a.cose...)
	return buf
}

func (a *softAuthn) sign(authData, clientDataJSON []byte) []byte {
	cdHash := sha256.Sum256(clientDataJSON)
	msg := append(append([]byte{}, authData...), cdHash[:]...)
	digest := sha256.Sum256(msg)
	sig, err := ecdsa.SignASN1(rand.Reader, a.priv, digest[:])
	if err != nil {
		panic(err)
	}
	return sig
}

func clientDataJSON(typ, challenge, origin string) []byte {
	b, err := json.Marshal(struct {
		Type      string `json:"type"`
		Challenge string `json:"challenge"`
		Origin    string `json:"origin"`
	}{typ, challenge, origin})
	if err != nil {
		panic(err)
	}
	return b
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func (a *softAuthn) registrationBody(challenge, origin, rpID string) []byte {
	a.rpID = rpID
	cd := clientDataJSON("webauthn.create", challenge, origin)
	auth := a.authData(0, true)
	em, err := cbor.CTAP2EncOptions().EncMode()
	if err != nil {
		panic(err)
	}
	att, err := em.Marshal(map[string]any{
		"fmt":      "none",
		"attStmt":  map[string]any{},
		"authData": auth,
	})
	if err != nil {
		panic(err)
	}
	body, err := json.Marshal(map[string]any{
		"id":    b64(a.credID),
		"rawId": b64(a.credID),
		"type":  "public-key",
		"response": map[string]any{
			"clientDataJSON":    b64(cd),
			"attestationObject": b64(att),
		},
	})
	if err != nil {
		panic(err)
	}
	return body
}

func (a *softAuthn) assertionBody(challenge, origin, rpID string) []byte {
	a.rpID = rpID
	a.counter++
	cd := clientDataJSON("webauthn.get", challenge, origin)
	auth := a.authData(a.counter, false)
	sig := a.sign(auth, cd)
	body, err := json.Marshal(map[string]any{
		"id":    b64(a.credID),
		"rawId": b64(a.credID),
		"type":  "public-key",
		"response": map[string]any{
			"clientDataJSON":    b64(cd),
			"authenticatorData": b64(auth),
			"signature":         b64(sig),
			"userHandle":        b64(a.user),
		},
	})
	if err != nil {
		panic(err)
	}
	return body
}
