//go:build libpcsclite
// +build libpcsclite

/*
Copyright 2022 Gravitational, Inc.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package keys

import (
	"crypto"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"strings"
	"time"

	"github.com/go-piv/piv-go/piv"
	"github.com/gravitational/trace"

	"github.com/gravitational/teleport/api"
)

const (
	// PIVCardTypeYubiKey is the PIV card type assigned to yubiKeys.
	PIVCardTypeYubiKey = "yubikey"
)

var (
	// We use slot 9a for Teleport Clients which require `private_key_policy: hardware_key`.
	pivSlotNoTouch = piv.SlotAuthentication
	// We use slot 9c for Teleport Clients which require `private_key_policy: hardware_key_touch`.
	pivSlotWithTouch = piv.SlotSignature
)

// GetOrGenerateYubiKeyPrivateKey connects to a connected yubiKey and gets a private key
// matching the given touch requirement. This private key will either be newly generated
// or previously generated by a Teleport client and reused.
func GetOrGenerateYubiKeyPrivateKey(touchRequired bool) (*PrivateKey, error) {
	// Use the first yubiKey we find.
	y, err := findYubiKey(0)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	// Get the correct PIV slot and Touch policy for the given touch requirement:
	//  - Slot 9a = hardware_key
	//  - Slot 9c = hardware_key_touch
	pivSlot := piv.SlotAuthentication
	touchPolicy := piv.TouchPolicyNever
	if touchRequired {
		pivSlot = piv.SlotSignature
		touchPolicy = piv.TouchPolicyCached
	}

	// First, check if there is already a private key set up by a Teleport Client.
	priv, err := y.getPrivateKey(pivSlot)
	if err != nil {
		// Generate a new private key on the PIV slot.
		if priv, err = y.generatePrivateKey(pivSlot, touchPolicy); err != nil {
			return nil, trace.Wrap(err)
		}
	}

	keyPEM, err := priv.keyPEM()
	if err != nil {
		return nil, trace.Wrap(err)
	}

	return NewPrivateKey(priv, keyPEM)
}

// YubiKeyPrivateKey is a YubiKey PIV private key. Cryptographical operations open
// a new temporary connection to the PIV card to perform the operation.
type YubiKeyPrivateKey struct {
	// yubiKey is a specific yubiKey PIV module.
	*yubiKey
	pivSlot piv.Slot
	pub     crypto.PublicKey
}

// yubiKeyPrivateKeyData is marshalable data used to retrieve a specific yubiKey PIV private key.
type yubiKeyPrivateKeyData struct {
	SerialNumber uint32 `json:"serial_number"`
	SlotKey      uint32 `json:"slot_key"`
}

func newYubiKeyPrivateKey(y *yubiKey, slot piv.Slot, pub crypto.PublicKey) (*YubiKeyPrivateKey, error) {
	return &YubiKeyPrivateKey{
		yubiKey: y,
		pivSlot: slot,
		pub:     pub,
	}, nil
}

func parseYubiKeyPrivateKeyData(keyDataBytes []byte) (*YubiKeyPrivateKey, error) {
	var keyData yubiKeyPrivateKeyData
	if err := json.Unmarshal(keyDataBytes, &keyData); err != nil {
		return nil, trace.Wrap(err)
	}

	pivSlot, err := parsePIVSlot(keyData.SlotKey)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	y, err := findYubiKey(keyData.SerialNumber)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	priv, err := y.getPrivateKey(pivSlot)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	return priv, nil
}

// Public returns the public key corresponding to this private key.
func (y *YubiKeyPrivateKey) Public() crypto.PublicKey {
	return y.pub
}

// Sign implements crypto.Signer.
func (y *YubiKeyPrivateKey) Sign(rand io.Reader, digest []byte, opts crypto.SignerOpts) (signature []byte, err error) {
	yk, err := y.open()
	if err != nil {
		return nil, trace.Wrap(err)
	}
	defer yk.Close()

	privateKey, err := yk.PrivateKey(y.pivSlot, y.pub, piv.KeyAuth{})
	if err != nil {
		return nil, trace.Wrap(err)
	}

	return privateKey.(crypto.Signer).Sign(rand, digest, opts)
}

func (y *YubiKeyPrivateKey) keyPEM() ([]byte, error) {
	keyDataBytes, err := json.Marshal(yubiKeyPrivateKeyData{
		SerialNumber: y.serialNumber,
		SlotKey:      y.pivSlot.Key,
	})
	if err != nil {
		return nil, trace.Wrap(err)
	}

	return pem.EncodeToMemory(&pem.Block{
		Type:    pivYubiKeyPrivateKeyType,
		Headers: nil,
		Bytes:   keyDataBytes,
	}), nil
}

// GetAttestationCerts gets a YubiKey PIV slot's attestation certificates.
func (y *YubiKeyPrivateKey) GetAttestationCerts() (slotCert, attestationCert *x509.Certificate, err error) {
	yk, err := y.open()
	if err != nil {
		return nil, nil, trace.Wrap(err)
	}
	defer yk.Close()

	slotCert, err = yk.Attest(y.pivSlot)
	if err != nil {
		return nil, nil, trace.Wrap(err)
	}

	attestationCert, err = yk.AttestationCertificate()
	if err != nil {
		return nil, nil, trace.Wrap(err)
	}

	return slotCert, attestationCert, nil
}

// yubiKey is a specific yubiKey PIV card.
type yubiKey struct {
	// card is a reader name used to find and connect to this yubiKey.
	// This value may change between OS's, or with other system changes.
	card string
	// serialNumber is the yubiKey's 8 digit serial number.
	serialNumber uint32
}

func newYubiKey(card string) (*yubiKey, error) {
	y := &yubiKey{card: card}

	yk, err := y.open()
	if err != nil {
		return nil, trace.Wrap(err)
	}
	defer yk.Close()

	y.serialNumber, err = yk.Serial()
	if err != nil {
		return nil, trace.Wrap(err)
	}

	return y, nil
}

// generatePrivateKey generates a new private key from the given PIV slot with the given PIV policies.
func (y *yubiKey) generatePrivateKey(slot piv.Slot, touchPolicy piv.TouchPolicy) (*YubiKeyPrivateKey, error) {
	yk, err := y.open()
	if err != nil {
		return nil, trace.Wrap(err)
	}
	defer yk.Close()

	opts := piv.Key{
		Algorithm:   piv.AlgorithmEC256,
		PINPolicy:   piv.PINPolicyNever,
		TouchPolicy: touchPolicy,
	}
	pub, err := yk.GenerateKey(piv.DefaultManagementKey, slot, opts)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	// Create a self signed certificate and store it in the PIV slot so that other
	// Teleport Clients know to reuse the stored key instead of genearting a new one.
	priv, err := yk.PrivateKey(slot, pub, piv.KeyAuth{})
	if err != nil {
		return nil, trace.Wrap(err)
	}

	cert, err := selfSignedTeleportClientCertificate(priv, pub)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	// Store a self-signed certificate to mark this slot as used by tsh.
	if err = yk.SetCertificate(piv.DefaultManagementKey, slot, cert); err != nil {
		return nil, trace.Wrap(err)
	}

	return newYubiKeyPrivateKey(y, slot, pub)
}

// getPrivateKey gets an existing private key from the given PIV slot.
func (y *yubiKey) getPrivateKey(slot piv.Slot) (*YubiKeyPrivateKey, error) {
	yk, err := y.open()
	if err != nil {
		return nil, trace.Wrap(err)
	}
	defer yk.Close()

	// Check the slot's certificate to see if it contains a self signed Teleport Client cert.
	cert, err := yk.Certificate(slot)
	if err != nil || cert == nil {
		return nil, trace.NotFound("YubiKey certificate slot is empty, expected a Teleport Client cert")
	} else if len(cert.Subject.Organization) == 0 || cert.Subject.Organization[0] != certOrgName {
		return nil, trace.NotFound("YubiKey certificate slot contained unknown certificate:\n%+v", cert)
	}

	// Attest the key to make sure it hasn't been imported.
	slotCert, err := yk.Attest(slot)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	attestationCert, err := yk.AttestationCertificate()
	if err != nil {
		return nil, trace.Wrap(err)
	}
	if _, err = piv.Verify(attestationCert, slotCert); err != nil {
		return nil, trace.Wrap(err)
	}

	// Verify that the slot's certs have the same public key, otherwise the key
	// may have been generated by a non-teleport client.
	if pubComparer, ok := cert.PublicKey.(interface{ Equal(x crypto.PublicKey) bool }); !ok {
		return nil, trace.BadParameter("certificate's public key of type %T is not a supported public key", cert.PublicKey)
	} else if !pubComparer.Equal(slotCert.PublicKey) {
		return nil, trace.NotFound("YubiKey slot contains mismatched certificates and must be regenerated")
	}

	return newYubiKeyPrivateKey(y, slot, slotCert.PublicKey)
}

// open a connection to yubiKey PIV module. The returned connection should be closed once
// it's been used. The yubiKey PIV module itself takes some additional time to handle closed
// connections, so we use a retry loop to give the PIV module time to close prior connections.
func (y *yubiKey) open() (yk *piv.YubiKey, err error) {
	isRetryError := func(err error) bool {
		retryError := "connecting to smart card: the smart card cannot be accessed because of other connections outstanding"
		return strings.Contains(err.Error(), retryError)
	}

	const maxRetries = 100
	for i := 0; i < maxRetries; i++ {
		yk, err = piv.Open(y.card)
		if err == nil {
			return yk, nil
		}

		if !isRetryError(err) {
			return nil, trace.Wrap(err)
		}

		time.Sleep(time.Millisecond * 100)
	}

	return nil, trace.Wrap(err)
}

// findYubiKey finds a yubiKey PIV card by serial number. If no serial
// number is provided, the first yubiKey found will be returned.
func findYubiKey(serialNumber uint32) (*yubiKey, error) {
	yubiKeyCards, err := findYubiKeyCards()
	if err != nil {
		return nil, trace.Wrap(err)
	}

	if len(yubiKeyCards) == 0 {
		return nil, trace.NotFound("no yubiKey devices found")
	}

	for _, card := range yubiKeyCards {
		y, err := newYubiKey(card)
		if err != nil {
			return nil, trace.Wrap(err)
		}

		if serialNumber == 0 || y.serialNumber == serialNumber {
			return y, nil
		}
	}

	return nil, trace.NotFound("no yubiKey device found with serial number %q", serialNumber)
}

// findYubiKeyCards returns a list of connected yubiKey PIV card names.
func findYubiKeyCards() ([]string, error) {
	cards, err := piv.Cards()
	if err != nil {
		return nil, trace.Wrap(err)
	}

	var yubiKeyCards []string
	for _, card := range cards {
		if strings.Contains(strings.ToLower(card), PIVCardTypeYubiKey) {
			yubiKeyCards = append(yubiKeyCards, card)
		}
	}

	return yubiKeyCards, nil
}

func parsePIVSlot(slotKey uint32) (piv.Slot, error) {
	switch slotKey {
	case piv.SlotAuthentication.Key:
		return piv.SlotAuthentication, nil
	case piv.SlotSignature.Key:
		return piv.SlotSignature, nil
	case piv.SlotCardAuthentication.Key:
		return piv.SlotCardAuthentication, nil
	case piv.SlotKeyManagement.Key:
		return piv.SlotKeyManagement, nil
	default:
		retiredSlot, ok := piv.RetiredKeyManagementSlot(slotKey)
		if !ok {
			return piv.Slot{}, trace.BadParameter("slot %X does not exist", slotKey)
		}
		return retiredSlot, nil
	}
}

// certOrgName is used to identify Teleport Client self-signed certificates stored in yubiKey PIV slots.
const certOrgName = "teleport"

func selfSignedTeleportClientCertificate(priv crypto.PrivateKey, pub crypto.PublicKey) (*x509.Certificate, error) {
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit) // see crypto/tls/generate_cert.go
	if err != nil {
		return nil, trace.Wrap(err)
	}
	cert := &x509.Certificate{
		SerialNumber: serialNumber,
		PublicKey:    pub,
		Subject: pkix.Name{
			Organization:       []string{certOrgName},
			OrganizationalUnit: []string{api.Version},
		},
	}
	if cert.Raw, err = x509.CreateCertificate(rand.Reader, cert, cert, pub, priv); err != nil {
		return nil, trace.Wrap(err)
	}
	return cert, nil
}
