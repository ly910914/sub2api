//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPrismAccountEditPersistence(t *testing.T) {
	cookie := prismTestCookie(time.Now().Add(time.Hour).Unix())
	repo := &updateAccountCredsRepoStub{account: &Account{ID: 71, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Credentials: map[string]any{"access_token": "codex-token", prismCookieKey: cookie}}}
	svc := &adminServiceImpl{accountRepo: repo}
	updated, err := svc.UpdateAccount(context.Background(), 71, &UpdateAccountInput{Extra: map[string]any{prismEnabledKey: true}, Credentials: map[string]any{"model_mapping": map[string]any{"alias": "gpt-6.1-sol"}}})
	require.NoError(t, err)
	require.True(t, updated.IsPrismEnabled())
	require.Equal(t, cookie, updated.GetCredential(prismCookieKey))
	require.Equal(t, "codex-token", updated.GetCredential("access_token"))
	updated, err = svc.UpdateAccount(context.Background(), 71, &UpdateAccountInput{Extra: map[string]any{prismEnabledKey: false}})
	require.NoError(t, err)
	require.False(t, updated.IsPrismEnabled())
	require.Equal(t, cookie, updated.GetCredential(prismCookieKey))
	require.Error(t, svc.UpdateAccountExtra(context.Background(), 71, map[string]any{prismEnabledKey: true}))
	_, err = svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{71}, Extra: map[string]any{prismEnabledKey: true}})
	require.Error(t, err)
	_, err = svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{71}, Credentials: map[string]any{prismCookieKey: cookie}})
	require.Error(t, err)
}
