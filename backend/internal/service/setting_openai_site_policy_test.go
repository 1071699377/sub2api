package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// sitePolicySettingRepoStub 是只实现 GetMultiple/SetMultiple 的内存设置仓库。
type sitePolicySettingRepoStub struct {
	values map[string]string
}

func (s *sitePolicySettingRepoStub) Get(ctx context.Context, key string) (*Setting, error) {
	panic("unused")
}
func (s *sitePolicySettingRepoStub) GetValue(ctx context.Context, key string) (string, error) {
	if v, ok := s.values[key]; ok {
		return v, nil
	}
	return "", ErrSettingNotFound
}
func (s *sitePolicySettingRepoStub) Set(ctx context.Context, key, value string) error {
	s.values[key] = value
	return nil
}
func (s *sitePolicySettingRepoStub) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	out := map[string]string{}
	for _, key := range keys {
		if v, ok := s.values[key]; ok {
			out[key] = v
		}
	}
	return out, nil
}
func (s *sitePolicySettingRepoStub) SetMultiple(ctx context.Context, settings map[string]string) error {
	for k, v := range settings {
		s.values[k] = v
	}
	return nil
}
func (s *sitePolicySettingRepoStub) GetAll(ctx context.Context) (map[string]string, error) {
	panic("unused")
}
func (s *sitePolicySettingRepoStub) Delete(ctx context.Context, key string) error {
	panic("unused")
}

func TestGetOpenAISitePolicyDefaultsWhenUnset(t *testing.T) {
	svc := NewSettingService(&sitePolicySettingRepoStub{values: map[string]string{}}, &config.Config{})
	policy, err := svc.GetOpenAISitePolicy(context.Background())
	require.NoError(t, err)
	require.Equal(t, DefaultOpenAISitePolicy(), policy)
	require.False(t, policy.ImageGenerationEnabled)
}

func TestUpdateOpenAISitePolicyPersistsAndAppliesImmediately(t *testing.T) {
	t.Cleanup(resetOpenAISitePolicyForTest)
	repo := &sitePolicySettingRepoStub{values: map[string]string{}}
	svc := NewSettingService(repo, &config.Config{})

	saved, err := svc.UpdateOpenAISitePolicy(context.Background(), OpenAISitePolicy{
		AllowedModels:          []string{" gpt-6-sol ", "gpt-6-sol", "gpt-5.6"},
		ImageGenerationEnabled: true,
	})
	require.NoError(t, err)
	require.Equal(t, []string{"gpt-6-sol", "gpt-5.6"}, saved.AllowedModels)
	require.Equal(t, `["gpt-6-sol","gpt-5.6"]`, repo.values[SettingKeyOpenAISiteAllowedModels])
	require.Equal(t, "true", repo.values[SettingKeyOpenAISiteImageGenerationEnabled])

	// 保存后本进程立即生效。
	require.True(t, OpenAISiteAllowsModel("gpt-5.6"))
	require.False(t, OpenAISiteAllowsModel("gpt-6-astra"))
	require.True(t, OpenAISiteImageGenerationEnabled())

	loaded, err := svc.GetOpenAISitePolicy(context.Background())
	require.NoError(t, err)
	require.Equal(t, saved, loaded)
}

func TestUpdateOpenAISitePolicyEmptyListIsUnrestricted(t *testing.T) {
	t.Cleanup(resetOpenAISitePolicyForTest)
	repo := &sitePolicySettingRepoStub{values: map[string]string{}}
	svc := NewSettingService(repo, &config.Config{})

	_, err := svc.UpdateOpenAISitePolicy(context.Background(), OpenAISitePolicy{AllowedModels: nil})
	require.NoError(t, err)
	require.Equal(t, `[]`, repo.values[SettingKeyOpenAISiteAllowedModels])
	require.False(t, OpenAISiteModelsRestricted())
	require.False(t, OpenAISiteImageGenerationEnabled())

	loaded, err := svc.GetOpenAISitePolicy(context.Background())
	require.NoError(t, err)
	require.Empty(t, loaded.AllowedModels, "保存过的空列表不能回落为默认两款模型")
}

func TestUpdateOpenAISitePolicyRejectsInvalidModelID(t *testing.T) {
	svc := NewSettingService(&sitePolicySettingRepoStub{values: map[string]string{}}, &config.Config{})
	_, err := svc.UpdateOpenAISitePolicy(context.Background(), OpenAISitePolicy{AllowedModels: []string{"gpt 6"}})
	require.Error(t, err)
}

func TestGetOpenAISitePolicyRejectsCorruptJSON(t *testing.T) {
	svc := NewSettingService(&sitePolicySettingRepoStub{values: map[string]string{
		SettingKeyOpenAISiteAllowedModels: "{bad",
	}}, &config.Config{})
	policy, err := svc.GetOpenAISitePolicy(context.Background())
	require.Error(t, err)
	// 解析失败时回落默认（严格）策略。
	require.Equal(t, DefaultOpenAISitePolicy().AllowedModels, policy.AllowedModels)
}
