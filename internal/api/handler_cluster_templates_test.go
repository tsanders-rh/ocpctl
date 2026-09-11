package api_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"github.com/tsanders-rh/ocpctl/internal/api"
	"github.com/tsanders-rh/ocpctl/internal/dbtest"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

func newTemplateUser(t *testing.T, s *store.Store) *types.User {
	t.Helper()
	id := uuid.New().String()
	user := &types.User{
		ID:           id,
		Email:        id + "@example.com",
		Username:     id,
		PasswordHash: "x",
		Role:         types.RoleUser,
		Timezone:     "UTC",
		WorkDays:     62,
		Active:       true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	require.NoError(t, s.Users.Create(ctx, user))
	return user
}

func TestClusterTemplateHandler_Create_EnforcesLimit(t *testing.T) {
	s := dbtest.New(t)
	user := newTemplateUser(t, s)
	h := api.NewClusterTemplateHandler(s)

	e := echo.New()
	e.Validator = api.NewValidator()

	post := func(name string) *httptest.ResponseRecorder {
		body := fmt.Sprintf(`{"name":%q,"config":{"platform":"aws"}}`, name)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/cluster-templates", strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		setAuthContext(c, user)
		require.NoError(t, h.Create(c))
		return rec
	}

	for i := 0; i < store.MaxTemplatesPerUser; i++ {
		rec := post(fmt.Sprintf("template-%02d", i))
		require.Equal(t, http.StatusCreated, rec.Code, "create %d should succeed", i)
	}

	rec := post("over-the-limit")
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), fmt.Sprintf("at most %d templates", store.MaxTemplatesPerUser))
}

func TestClusterTemplateHandler_Create_StripsName(t *testing.T) {
	s := dbtest.New(t)
	user := newTemplateUser(t, s)
	h := api.NewClusterTemplateHandler(s)

	e := echo.New()
	e.Validator = api.NewValidator()

	body := `{"name":"has-name","config":{"name":"should-be-stripped","platform":"aws"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cluster-templates", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	setAuthContext(c, user)
	require.NoError(t, h.Create(c))
	require.Equal(t, http.StatusCreated, rec.Code)

	list, err := s.ClusterTemplates.List(ctx, user.ID)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.NotContains(t, string(list[0].Config), "should-be-stripped")
	require.NotContains(t, string(list[0].Config), `"name"`)
}

func TestClusterTemplateHandler_Create_StripsPullSecret(t *testing.T) {
	s := dbtest.New(t)
	user := newTemplateUser(t, s)
	h := api.NewClusterTemplateHandler(s)

	e := echo.New()
	e.Validator = api.NewValidator()

	body := `{"name":"has-secret","config":{"custom_pull_secret":"super-secret-token","platform":"aws"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cluster-templates", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	setAuthContext(c, user)
	require.NoError(t, h.Create(c))
	require.Equal(t, http.StatusCreated, rec.Code)

	list, err := s.ClusterTemplates.List(ctx, user.ID)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.NotContains(t, string(list[0].Config), "super-secret-token")
	require.NotContains(t, string(list[0].Config), "custom_pull_secret")
	require.Contains(t, string(list[0].Config), "aws")
}
