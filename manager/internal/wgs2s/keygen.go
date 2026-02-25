package wgs2s

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

func generateKeypair(configDir, id string) (wgtypes.Key, error) {
	privKey, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return wgtypes.Key{}, fmt.Errorf("generate private key: %w", err)
	}
	return saveKeyFiles(configDir, id, privKey)
}

func saveExistingKeypair(configDir, id, privKeyStr string) (wgtypes.Key, error) {
	privKey, err := wgtypes.ParseKey(strings.TrimSpace(privKeyStr))
	if err != nil {
		return wgtypes.Key{}, fmt.Errorf("parse private key: %w", err)
	}
	return saveKeyFiles(configDir, id, privKey)
}

func saveKeyFiles(configDir, id string, privKey wgtypes.Key) (wgtypes.Key, error) {
	keyPath := filepath.Join(configDir, id+".key")
	if err := os.WriteFile(keyPath, []byte(privKey.String()), 0600); err != nil {
		return wgtypes.Key{}, fmt.Errorf("write private key: %w", err)
	}

	pubPath := filepath.Join(configDir, id+".pub")
	if err := os.WriteFile(pubPath, []byte(privKey.PublicKey().String()), 0600); err != nil {
		return wgtypes.Key{}, fmt.Errorf("write public key: %w", err)
	}

	return privKey, nil
}

func loadPrivateKey(configDir, id string) (wgtypes.Key, error) {
	data, err := os.ReadFile(filepath.Join(configDir, id+".key"))
	if err != nil {
		return wgtypes.Key{}, fmt.Errorf("read private key: %w", err)
	}
	return wgtypes.ParseKey(string(data))
}

func deleteKeyFiles(configDir, id string) {
	_ = os.Remove(filepath.Join(configDir, id+".key"))
	_ = os.Remove(filepath.Join(configDir, id+".pub"))
}
