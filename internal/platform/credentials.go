package platform

import (
	"encoding/json"
	"fmt"
	"slices"
)

type keychainBackend interface {
	get(service, account string) ([]byte, error)
	set(service, account string, data []byte) error
	del(service, account string) error
}

type isolatedCredentialStore struct {
	backend keychainBackend
}

type credentialIndex struct {
	Selected   string   `json:"selected,omitempty"`
	AccountIDs []string `json:"accountIds,omitempty"`
}

type rawAccountRecord struct {
	Selected string            `json:"selected,omitempty"`
	Accounts []json.RawMessage `json:"accounts"`
}

type accountEnvelope struct {
	Account struct {
		ID string `json:"id"`
	} `json:"account"`
}

func credentialService(pluginID string) string {
	return "mikutv.source." + pluginID
}

func credentialIndexAccount(entryID string) string {
	return "v2:" + entryID + ":index"
}

func credentialItemAccount(entryID, accountID string) string {
	return "v2:" + entryID + ":account:" + accountID
}

func (s *isolatedCredentialStore) Load(pluginID, entryID string) ([]byte, error) {
	service := credentialService(pluginID)
	indexData, err := s.backend.get(service, credentialIndexAccount(entryID))
	if err != nil {
		return nil, err
	}
	if len(indexData) > 0 {
		var index credentialIndex
		if err := json.Unmarshal(indexData, &index); err != nil {
			return nil, fmt.Errorf("decode credential index: %w", err)
		}
		record := rawAccountRecord{Accounts: make([]json.RawMessage, 0, len(index.AccountIDs))}
		loadedIDs := make(map[string]bool, len(index.AccountIDs))
		for _, id := range index.AccountIDs {
			if id == "" {
				continue
			}
			item, err := s.backend.get(service, credentialItemAccount(entryID, id))
			if err != nil {
				return nil, err
			}
			if len(item) == 0 || !json.Valid(item) {
				// Isolate corrupt or missing individual accounts so sibling accounts remain usable.
				continue
			}
			var env accountEnvelope
			if err := json.Unmarshal(item, &env); err != nil || env.Account.ID == "" {
				continue
			}
			record.Accounts = append(record.Accounts, json.RawMessage(item))
			loadedIDs[env.Account.ID] = true
		}
		if len(record.Accounts) == 0 {
			return nil, nil
		}
		if loadedIDs[index.Selected] {
			record.Selected = index.Selected
		}
		return json.Marshal(record)
	}

	// Migrate legacy single-blob entry storage seamlessly on first read.
	legacy, err := s.backend.get(service, entryID)
	if err != nil || len(legacy) == 0 {
		return legacy, err
	}
	_ = s.Save(pluginID, entryID, legacy)
	return legacy, nil
}

func (s *isolatedCredentialStore) Save(pluginID, entryID string, data []byte) error {
	service := credentialService(pluginID)
	oldIndex := s.readIndexBestEffort(service, entryID)

	if len(data) == 0 {
		var firstErr error
		for _, id := range oldIndex.AccountIDs {
			if id == "" {
				continue
			}
			if err := s.backend.del(service, credentialItemAccount(entryID, id)); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		if err := s.backend.del(service, credentialIndexAccount(entryID)); err != nil && firstErr == nil {
			firstErr = err
		}
		if err := s.backend.del(service, entryID); err != nil && firstErr == nil {
			firstErr = err
		}
		return firstErr
	}

	var record rawAccountRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return fmt.Errorf("decode credential record: %w", err)
	}
	ids := make([]string, 0, len(record.Accounts))
	seen := make(map[string]bool, len(record.Accounts))
	for _, raw := range record.Accounts {
		var env accountEnvelope
		if err := json.Unmarshal(raw, &env); err != nil {
			return fmt.Errorf("decode account credential: %w", err)
		}
		if env.Account.ID == "" {
			return fmt.Errorf("credential account id is empty")
		}
		if seen[env.Account.ID] {
			continue
		}
		seen[env.Account.ID] = true
		ids = append(ids, env.Account.ID)
		if err := s.backend.set(service, credentialItemAccount(entryID, env.Account.ID), raw); err != nil {
			return err
		}
	}
	indexBytes, err := json.Marshal(credentialIndex{
		Selected:   record.Selected,
		AccountIDs: ids,
	})
	if err != nil {
		return err
	}
	if err := s.backend.set(service, credentialIndexAccount(entryID), indexBytes); err != nil {
		return err
	}
	for _, oldID := range oldIndex.AccountIDs {
		if oldID != "" && !slices.Contains(ids, oldID) {
			_ = s.backend.del(service, credentialItemAccount(entryID, oldID))
		}
	}
	_ = s.backend.del(service, entryID)
	return nil
}

func (s *isolatedCredentialStore) readIndexBestEffort(service, entryID string) credentialIndex {
	data, err := s.backend.get(service, credentialIndexAccount(entryID))
	if err != nil || len(data) == 0 {
		return credentialIndex{}
	}
	var idx credentialIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return credentialIndex{}
	}
	return idx
}
