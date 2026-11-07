//go:build unit

package repository

import (
	"context"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
)

func TestSelectUnusedActiveProxiesOneToOne(t *testing.T) {
	db := scopedAdminKeyTestDB(t)
	_, err := db.Exec(`CREATE TABLE proxies(
 id BIGINT PRIMARY KEY,name TEXT DEFAULT '',protocol TEXT DEFAULT 'http',host TEXT DEFAULT '192.0.2.1',port INT DEFAULT 8080,
 username TEXT DEFAULT '',password TEXT DEFAULT '',status TEXT DEFAULT 'active',
 expires_at TIMESTAMPTZ,deleted_at TIMESTAMPTZ,created_at TIMESTAMPTZ DEFAULT NOW(),updated_at TIMESTAMPTZ DEFAULT NOW(),
 fallback_mode TEXT DEFAULT 'none',backup_proxy_id BIGINT,expiry_warn_days INT DEFAULT 7);
 CREATE TABLE accounts(id BIGINT PRIMARY KEY,proxy_id BIGINT,deleted_at TIMESTAMPTZ);
 INSERT INTO proxies(id) SELECT generate_series(1,6);
 UPDATE proxies SET status='inactive' WHERE id=1;
 UPDATE proxies SET expires_at=NOW()-INTERVAL '1 hour' WHERE id=2;
 UPDATE proxies SET deleted_at=NOW() WHERE id=3;
 INSERT INTO accounts VALUES(1,4,NULL),(2,5,NOW());
`)
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	repo := newProxyRepositoryWithSQL(client, db)

	count, err := repo.CountUnusedActiveProxies(context.Background())
	require.NoError(t, err)
	// active unused: id=5 (soft-deleted account does not bind), id=6
	require.EqualValues(t, 2, count)

	_, err = repo.SelectUnusedActiveProxies(context.Background(), 2)
	require.Error(t, err)

	tx, err := client.Tx(context.Background())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	ctx := dbent.NewTxContext(context.Background(), tx)

	ids, err := repo.SelectUnusedActiveProxies(ctx, 2)
	require.NoError(t, err)
	require.Equal(t, []int64{5, 6}, ids)

	ids, err = repo.SelectUnusedActiveProxies(ctx, 3)
	require.NoError(t, err)
	require.Equal(t, []int64{5, 6}, ids)
	require.Less(t, len(ids), 3)

	ids, err = repo.SelectUnusedActiveProxies(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, []int64{5}, ids)

	require.NoError(t, tx.Rollback())
}
