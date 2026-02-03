// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package auth

import (
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"

	"github.com/crewjam/saml"
	"github.com/crewjam/saml/samlsp"
)

// SAMLConfig holds SAML configuration
type SAMLConfig struct {
	EntityID     string
	ACSURL       string
	MetadataURL  string
	IDPMetadata  string
	Certificate  string
	PrivateKey   string
	Provider     string // "duo" or "okta"
}

// SAMLAuthenticator handles SAML authentication
type SAMLAuthenticator struct {
	sp       *saml.ServiceProvider
	config   *SAMLConfig
	middleware *samlsp.Middleware
}

// SAMLUser represents a user from SAML assertion
type SAMLUser struct {
	Username   string
	Email      string
	Attributes map[string][]string
}

// NewSAMLAuthenticator creates a new SAML authenticator
func NewSAMLAuthenticator(config *SAMLConfig) (*SAMLAuthenticator, error) {
	// Parse certificate and private key
	keyPair, err := parseCertificateAndKey(config.Certificate, config.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate and key: %w", err)
	}

	// Parse entity ID as URL
	rootURL, err := url.Parse(config.EntityID)
	if err != nil {
		return nil, fmt.Errorf("failed to parse entity ID: %w", err)
	}

	// Parse IDP metadata
	idpMetadata, err := parseIDPMetadata(config.IDPMetadata)
	if err != nil {
		return nil, fmt.Errorf("failed to parse IDP metadata: %w", err)
	}

	// Create service provider
	sp := &saml.ServiceProvider{
		EntityID:    config.EntityID,
		Key:         keyPair.PrivateKey.(*rsa.PrivateKey),
		Certificate: keyPair.Leaf,
		MetadataURL: *mustParseURL(config.MetadataURL),
		AcsURL:      *mustParseURL(config.ACSURL),
		IDPMetadata: idpMetadata,
	}

	// Create middleware
	middleware, err := samlsp.New(samlsp.Options{
		URL:         *rootURL,
		Key:         sp.Key,
		Certificate: sp.Certificate,
		IDPMetadata: idpMetadata,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create SAML middleware: %w", err)
	}

	return &SAMLAuthenticator{
		sp:         sp,
		config:     config,
		middleware: middleware,
	}, nil
}

// InitiateLogin redirects the user to the IdP for authentication
func (s *SAMLAuthenticator) InitiateLogin(w http.ResponseWriter, r *http.Request) {
	// Use the middleware to handle the login
	s.middleware.HandleStartAuthFlow(w, r)
}

// HandleACS processes the SAML assertion from the IdP
func (s *SAMLAuthenticator) HandleACS(w http.ResponseWriter, r *http.Request) (*SAMLUser, error) {
	// Parse the SAML response
	err := r.ParseForm()
	if err != nil {
		return nil, fmt.Errorf("failed to parse form: %w", err)
	}

	// Get the SAML response
	samlResponse := r.FormValue("SAMLResponse")
	if samlResponse == "" {
		return nil, fmt.Errorf("missing SAMLResponse")
	}

	// Parse and validate the assertion
	assertion, err := s.sp.ParseResponse(r, []string{""})
	if err != nil {
		return nil, fmt.Errorf("failed to parse SAML response: %w", err)
	}

	// Extract user information
	user := &SAMLUser{
		Attributes: make(map[string][]string),
	}

	// Extract username (subject)
	if assertion.Subject != nil && assertion.Subject.NameID != nil {
		user.Username = assertion.Subject.NameID.Value
	}

	// Extract attributes
	if len(assertion.AttributeStatements) > 0 {
		for _, attr := range assertion.AttributeStatements[0].Attributes {
			values := make([]string, len(attr.Values))
			for i, v := range attr.Values {
				values[i] = v.Value
			}
			user.Attributes[attr.Name] = values

			// Extract common attributes
			switch attr.Name {
			case "email", "mail", "emailAddress":
				if len(values) > 0 {
					user.Email = values[0]
				}
			case "username", "uid", "user":
				if len(values) > 0 && user.Username == "" {
					user.Username = values[0]
				}
			}
		}
	}

	// If username is still empty, try to use email
	if user.Username == "" && user.Email != "" {
		user.Username = user.Email
	}

	if user.Username == "" {
		return nil, fmt.Errorf("unable to extract username from SAML assertion")
	}

	return user, nil
}

// GetMetadata returns the SP metadata XML
func (s *SAMLAuthenticator) GetMetadata() ([]byte, error) {
	metadata := s.sp.Metadata()
	return xml.MarshalIndent(metadata, "", "  ")
}

// ServeMetadata serves the SP metadata endpoint
func (s *SAMLAuthenticator) ServeMetadata(w http.ResponseWriter, r *http.Request) {
	metadata, err := s.GetMetadata()
	if err != nil {
		http.Error(w, "Failed to generate metadata", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/xml")
	_, _ = w.Write(metadata)
}

// parseCertificateAndKey parses PEM-encoded certificate and private key
func parseCertificateAndKey(certPEM, keyPEM string) (*tls.Certificate, error) {
	// Decode certificate
	certBlock, _ := pem.Decode([]byte(certPEM))
	if certBlock == nil {
		return nil, fmt.Errorf("failed to decode certificate PEM")
	}

	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	// Decode private key
	keyBlock, _ := pem.Decode([]byte(keyPEM))
	if keyBlock == nil {
		return nil, fmt.Errorf("failed to decode private key PEM")
	}

	key, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	if err != nil {
		// Try PKCS8 format
		keyInterface, err2 := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
		if err2 != nil {
			return nil, fmt.Errorf("failed to parse private key (tried PKCS1 and PKCS8): %w", err)
		}
		var ok bool
		key, ok = keyInterface.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("private key is not RSA")
		}
	}

	return &tls.Certificate{
		Certificate: [][]byte{cert.Raw},
		PrivateKey:  key,
		Leaf:        cert,
	}, nil
}

// parseIDPMetadata parses IDP metadata from XML string or URL
func parseIDPMetadata(metadata string) (*saml.EntityDescriptor, error) {
	// Try to parse as URL first
	if _, err := url.ParseRequestURI(metadata); err == nil {
		// It's a URL, fetch metadata
		resp, err := http.Get(metadata)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch IDP metadata: %w", err)
		}
		defer resp.Body.Close()

		var entityDescriptor saml.EntityDescriptor
		if err := xml.NewDecoder(resp.Body).Decode(&entityDescriptor); err != nil {
			return nil, fmt.Errorf("failed to decode IDP metadata: %w", err)
		}
		return &entityDescriptor, nil
	}

	// Try to parse as XML string
	var entityDescriptor saml.EntityDescriptor
	if err := xml.Unmarshal([]byte(metadata), &entityDescriptor); err != nil {
		return nil, fmt.Errorf("failed to parse IDP metadata XML: %w", err)
	}
	return &entityDescriptor, nil
}

// mustParseURL parses a URL and panics on error (for initialization)
func mustParseURL(rawURL string) *url.URL {
	u, err := url.Parse(rawURL)
	if err != nil {
		panic(fmt.Sprintf("failed to parse URL %s: %v", rawURL, err))
	}
	return u
}
