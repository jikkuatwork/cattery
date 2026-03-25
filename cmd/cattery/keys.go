package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/jikkuatwork/cattery/paths"
	"github.com/jikkuatwork/cattery/server"
)

func cmdKeys(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: cattery keys <create|list|revoke|delete>")
	}

	switch args[0] {
	case "create":
		return cmdKeysCreate(args[1:])
	case "list":
		return cmdKeysList(args[1:])
	case "revoke":
		return cmdKeysRevoke(args[1:])
	case "delete":
		return cmdKeysDelete(args[1:])
	default:
		return fmt.Errorf("unknown keys command %q", args[0])
	}
}

func cmdKeysCreate(args []string) error {
	name := ""
	rate := server.DefaultKeyRateLimit

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--name":
			i++
			if i >= len(args) {
				return fmt.Errorf("missing value for --name")
			}
			name = strings.TrimSpace(args[i])
		case "--rate":
			i++
			if i >= len(args) {
				return fmt.Errorf("missing value for --rate")
			}
			if _, err := fmt.Sscanf(args[i], "%d", &rate); err != nil {
				return fmt.Errorf("invalid rate %q", args[i])
			}
		default:
			return fmt.Errorf("unknown flag %q for keys create", args[i])
		}
	}

	if name == "" {
		return fmt.Errorf("keys create requires --name")
	}
	if rate < 0 {
		return fmt.Errorf("rate limit must be zero or greater")
	}

	store := newCLIKeyStore()
	entries, err := loadCLIKeys(store, true)
	if err != nil {
		return err
	}

	var (
		fullKey string
		entry   server.KeyEntry
	)
	for {
		fullKey, entry, err = server.GenerateKey()
		if err != nil {
			return err
		}
		if !keyIDExists(entries, entry.ID) {
			break
		}
	}

	entry.Name = name
	entry.RateLimit = rate
	entries = append(entries, entry)
	if err := store.Save(entries); err != nil {
		return err
	}

	fmt.Printf("id: %s\n", entry.ID)
	fmt.Printf("name: %s\n", entry.Name)
	fmt.Printf("rate_limit: %d\n", entry.RateLimit)
	fmt.Printf("key: %s\n", fullKey)
	return nil
}

func cmdKeysList(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("keys list takes no arguments")
	}

	store := newCLIKeyStore()
	entries, err := loadCLIKeys(store, true)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		fmt.Println("No keys found.")
		return nil
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tCREATED\tRATE\tDISABLED")
	for _, entry := range entries {
		fmt.Fprintf(
			tw,
			"%s\t%s\t%s\t%d\t%t\n",
			entry.ID,
			entry.Name,
			entry.Created.Format(timeLayoutSeconds),
			entry.RateLimit,
			entry.Disabled,
		)
	}
	return tw.Flush()
}

func cmdKeysRevoke(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: cattery keys revoke <id>")
	}

	store := newCLIKeyStore()
	entries, err := loadCLIKeys(store, false)
	if err != nil {
		return err
	}

	id := strings.TrimSpace(args[0])
	updated := false
	for i := range entries {
		if entries[i].ID != id {
			continue
		}
		entries[i].Disabled = true
		updated = true
		break
	}
	if !updated {
		return fmt.Errorf("unknown key %q", id)
	}
	if err := store.Save(entries); err != nil {
		return err
	}

	fmt.Printf("revoked %s\n", id)
	return nil
}

func cmdKeysDelete(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: cattery keys delete <id>")
	}

	store := newCLIKeyStore()
	entries, err := loadCLIKeys(store, false)
	if err != nil {
		return err
	}

	id := strings.TrimSpace(args[0])
	filtered := entries[:0]
	deleted := false
	for _, entry := range entries {
		if entry.ID == id {
			deleted = true
			continue
		}
		filtered = append(filtered, entry)
	}
	if !deleted {
		return fmt.Errorf("unknown key %q", id)
	}
	if err := store.Save(filtered); err != nil {
		return err
	}

	fmt.Printf("deleted %s\n", id)
	return nil
}

const timeLayoutSeconds = "2006-01-02 15:04:05Z07:00"

func newCLIKeyStore() *server.KeyStore {
	return server.NewKeyStore(filepath.Join(paths.DataDir(), "keys.json"))
}

func loadCLIKeys(store *server.KeyStore, allowMissing bool) ([]server.KeyEntry, error) {
	if err := store.Load(); err != nil {
		if allowMissing && errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("no API keys found; run 'cattery keys create' first")
		}
		return nil, err
	}
	return store.Entries(), nil
}

func keyIDExists(entries []server.KeyEntry, id string) bool {
	for _, entry := range entries {
		if entry.ID == id {
			return true
		}
	}
	return false
}
