package credentials

import (
	"encoding/json"
	"fmt"

	"github.com/zalando/go-keyring"
)

type keychainStore struct{}

func (s *keychainStore) Read(name string) (Token, error) {
	data, err := keyring.Get(keychainService, name)
	if err != nil {
		return Token{}, fmt.Errorf("reading keychain for %s: %w", name, err)
	}
	var tok Token
	if err := json.Unmarshal([]byte(data), &tok); err != nil {
		return Token{}, fmt.Errorf("parsing keychain data for %s: %w", name, err)
	}
	return tok, nil
}

func (s *keychainStore) Write(name string, tok Token) error {
	data, err := json.Marshal(tok)
	if err != nil {
		return err
	}
	return keyring.Set(keychainService, name, string(data))
}

func (s *keychainStore) Delete(name string) error {
	return keyring.Delete(keychainService, name)
}
