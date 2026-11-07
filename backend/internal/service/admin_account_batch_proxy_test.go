package service

import (
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestErrProxyPoolInsufficientMetadata(t *testing.T) {
	err := ErrProxyPoolInsufficient(5, 2)
	var appErr *infraerrors.ApplicationError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, "PROXY_POOL_INSUFFICIENT", appErr.Reason)
	require.Equal(t, "5", appErr.Metadata["needed"])
	require.Equal(t, "2", appErr.Metadata["available"])
	require.Contains(t, appErr.Message, "需要 5")
	require.Contains(t, appErr.Message, "可用 2")
}

func TestProxyAssignOneToOneConstant(t *testing.T) {
	require.Equal(t, "one_to_one", ProxyAssignOneToOne)
}
