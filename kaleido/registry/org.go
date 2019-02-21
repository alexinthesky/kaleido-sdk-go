package registry

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io/ioutil"
	"strings"

	"github.com/youmark/pkcs8"
	jose "gopkg.in/square/go-jose.v2"
)

// Organization ...
type Organization struct {
	Consortium     string `json:"consortia_id,omitempty"`
	Environment    string `json:"environment_id,omitempty"`
	MemberID       string `json:"membership_id,omitempty"`
	Name           string `json:"-"`
	Owner          string `json:"-"`
	SigningKeyFile string `json:"-"`
	CertPEMFile    string `json:"-"`
}

// VerifiedOrganization ...
type VerifiedOrganization struct {
	ID       string            `json:"id,omitempty"`
	Name     string            `json:"name,omitempty"`
	Owner    string            `json:"owner,omitempty"`
	Proof    *JSONWebSignature `json:"proof,omitempty"`
	ParentID string            `json:"parent,omitempty"`
}

// JSONWebSignature json representation of JWS
type JSONWebSignature struct {
	Headers    []string `json:"headers"`
	Payload    string   `json:"payload"`
	Signatures []string `json:"signatures"`
}

// SignedRequest signed payload for identity creation of an org
type SignedRequest struct {
	Consortium   string           `json:"consortia_id,omitempty"`
	Environment  string           `json:"environment_id,omitempty"`
	MembershipID string           `json:"membership_id,omitempty"`
	JWS          JSONWebSignature `json:"jwsjs,omitempty"`
}

func (org *Organization) generateNonce() (string, error) {
	type responseBody struct {
		Nonce string `json:"nonce,omitempty"`
	}

	client := utils().getAPIClient()
	var noncePayload responseBody
	response, err := client.R().
		SetHeader("Content-Type", "application/json").
		// SetBody(org).
		SetBody(org).
		SetResult(&noncePayload).
		Post("/nonce")

	if err != nil {
		return "", err
	}
	return noncePayload.Nonce, utils().validateCreateResponse(response, err, "nonce")
}

// sourced from go-ethereum
func zeroKey(k *ecdsa.PrivateKey) {
	b := k.D.Bits()
	for i := range b {
		b[i] = 0
	}
}

func (org *Organization) createSignedRequestForRegistration() (*SignedRequest, error) {
	request := SignedRequest{
		Consortium:   org.Consortium,
		Environment:  org.Environment,
		MembershipID: org.MemberID,
	}

	// read the key file
	pemEncodedBytes, err := ioutil.ReadFile(org.SigningKeyFile)
	if err != nil {
		return nil, err
	}

	block, _ := pem.Decode(pemEncodedBytes)
	der := block.Bytes

	var ecdsaKey *ecdsa.PrivateKey
	if strings.Contains(string(pemEncodedBytes), "-----BEGIN ENCRYPTED PRIVATE KEY-----") {
		passphrase, err := utils().readPassword("KLD_PKCS8_SIGNING_KEY_PASSPHRASE", "Encrypted signing PKCS8 key requires a password:")
		if err != nil {
			return nil, err
		}
		privateKey, err := pkcs8.ParsePKCS8PrivateKey(der, []byte(passphrase))
		if err != nil {
			return nil, err
		}
		ecdsaKey = privateKey.(*ecdsa.PrivateKey)
	} else {
		privateKey, err := pkcs8.ParsePKCS8PrivateKey(der)
		if err != nil {
			return nil, err
		}
		ecdsaKey = privateKey.(*ecdsa.PrivateKey)
	}
	defer zeroKey(ecdsaKey)

	// read the provided proof
	proofPEM, err := ioutil.ReadFile(org.CertPEMFile)
	if err != nil {
		return nil, err
	}

	certBlock, _ := pem.Decode(proofPEM)
	if certBlock == nil {
		return nil, errors.New("failed to parse certificate")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, err
	}

	CNTokens := strings.Split(cert.Subject.CommonName, "-")
	if len(CNTokens) != 4 {
		return nil, errors.New("common name does not follow the format of <orgid>-<nonce>--<name>")
	}

	preferedName := CNTokens[3] + "--" + CNTokens[0]
	if org.Name == "" {
		org.Name = preferedName
	}

	if !strings.Contains(org.Name, CNTokens[3]) || !strings.Contains(org.Name, CNTokens[0]) {
		return nil, fmt.Errorf("specified name does not match proof: must contain '%s' and '%s'. suggested name: %s", CNTokens[3], CNTokens[0], preferedName)
	}

	// create a new signer using ECDSA (ES256) algorithm with the given private key
	var alg jose.SignatureAlgorithm
	switch ecdsaKey.Curve.Params().BitSize {
	case 256:
		alg = jose.ES256
	case 384:
		alg = jose.ES384
	case 521: // not a typo, ES512 == 521 curve bits
		alg = jose.ES512
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: alg, Key: ecdsaKey}, nil)
	if err != nil {
		return nil, err
	}

	// create the json payload that needs to be signed
	nonce, err := org.generateNonce()
	if err != nil {
		return nil, err
	}

	jsonBytes, err := json.Marshal(map[string]interface{}{
		"envId":   org.Environment,
		"nonce":   nonce,
		"name":    org.Name,
		"proof":   string(proofPEM),
		"address": org.Owner})

	if err != nil {
		return nil, err
	}

	object, err := signer.Sign(jsonBytes)
	if err != nil {
		return nil, err
	}

	serialized, _ := object.CompactSerialize()
	tokens := strings.Split(serialized, ".")
	request.JWS.Headers = append(request.JWS.Headers, tokens[0])
	request.JWS.Payload = tokens[1]
	request.JWS.Signatures = append(request.JWS.Signatures, tokens[2])

	return &request, nil
}

func (org *Organization) populateServiceTargets() error {
	var service *serviceDefinitionType
	var err error
	if service, err = utils().getServiceDefinition(); err != nil {
		return err
	}
	org.Consortium = service.Consortium
	org.Environment = service.Environment
	org.MemberID = service.MemberID

	return nil
}

// InvokeCreate registers a verified organization with the on-chain registry
// and stores the proof on-chain
func (org *Organization) InvokeCreate() (*VerifiedOrganization, error) {
	// if consortium, environment, or member is not set, retrieve it from the service definition
	if org.Consortium == "" || org.Environment == "" || org.MemberID == "" {
		if err := org.populateServiceTargets(); err != nil {
			return nil, err
		}
	}

	// sign payload
	signedPayload, err := org.createSignedRequestForRegistration()
	if err != nil {
		return nil, err
	}

	client := utils().getAPIClient()

	var verifiedOrg VerifiedOrganization
	response, err := client.R().SetBody(signedPayload).SetResult(&verifiedOrg).Post("/identity")

	err = utils().validateCreateResponse(response, err, "identity")
	return &verifiedOrg, err
}

// InvokeGet retrieve an organization
func (org *Organization) InvokeGet() (*VerifiedOrganization, error) {
	client := utils().getDirectoryClient()

	nodeID := utils().generateNodeID(org.Name)

	var verifiedOrg VerifiedOrganization
	response, err := client.R().SetResult(&verifiedOrg).Get("/orgs/" + nodeID)

	err = utils().validateGetResponse(response, err, "org")
	return &verifiedOrg, err
}

// InvokeList retrieve a list of registered top-level organizations
func (org *Organization) InvokeList() (*[]VerifiedOrganization, error) {
	type responseBodyType struct {
		Count string                 `json:"count,omitempty"`
		Orgs  []VerifiedOrganization `json:"orgs,omitempty"`
	}
	var responseBody responseBodyType
	client := utils().getDirectoryClient()
	response, err := client.R().SetResult(&responseBody).Get("/orgs")

	err = utils().validateGetResponse(response, err, "orgs")
	return &responseBody.Orgs, err
}