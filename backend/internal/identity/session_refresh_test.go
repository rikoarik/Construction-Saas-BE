package identity

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestRotateSessionSingleUse(t *testing.T) {
	store, user := integrationIdentity(t)
	ctx := context.Background()
	original, err := store.CreateSession(ctx, user.ID, sessionTokenHash("refresh-original"), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	svc := &AuthService{Store: store}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	tokens := make(chan string, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			token, expiry, err := svc.RotateSession(ctx, original)
			if err == nil && !expiry.Equal(original.ExpiresAt) {
				t.Errorf("expiry changed")
			}
			results <- err
			tokens <- token
		}()
	}
	wg.Wait()
	close(results)
	close(tokens)
	success, denied := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrUnauthenticated) {
			denied++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || denied != 1 {
		t.Fatalf("success=%d denied=%d", success, denied)
	}
	if _, err = store.GetActiveSessionByTokenHash(ctx, sessionTokenHash("refresh-original")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old token still active: %v", err)
	}
	for token := range tokens {
		if token != "" {
			if _, err = store.GetActiveSessionByTokenHash(ctx, sessionTokenHash(token)); err != nil {
				t.Fatal(err)
			}
		}
	}
}
