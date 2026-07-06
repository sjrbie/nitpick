package config

import "testing"

func TestParseRemoteURL(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    Remote
		wantErr bool
	}{
		{
			name: "scp-style ssh with .git",
			raw:  "git@github.com:sjrbie/nitpick.git",
			want: Remote{Host: "github.com", Owner: "sjrbie", Name: "nitpick"},
		},
		{
			name: "scp-style ssh without .git",
			raw:  "git@github.com:sjrbie/nitpick",
			want: Remote{Host: "github.com", Owner: "sjrbie", Name: "nitpick"},
		},
		{
			name: "https with .git",
			raw:  "https://github.com/sjrbie/nitpick.git",
			want: Remote{Host: "github.com", Owner: "sjrbie", Name: "nitpick"},
		},
		{
			name: "https without .git and trailing slash",
			raw:  "https://github.com/sjrbie/nitpick/",
			want: Remote{Host: "github.com", Owner: "sjrbie", Name: "nitpick"},
		},
		{
			name: "ssh scheme with user",
			raw:  "ssh://git@github.com/sjrbie/nitpick.git",
			want: Remote{Host: "github.com", Owner: "sjrbie", Name: "nitpick"},
		},
		{
			name: "enterprise host",
			raw:  "https://ghe.example.com/team/app.git",
			want: Remote{Host: "ghe.example.com", Owner: "team", Name: "app"},
		},
		{
			name: "gitlab subgroup keeps last two segments",
			raw:  "git@gitlab.com:group/subgroup/app.git",
			want: Remote{Host: "gitlab.com", Owner: "subgroup", Name: "app"},
		},
		{name: "empty", raw: "", wantErr: true},
		{name: "garbage", raw: "not-a-url", wantErr: true},
		{name: "missing name", raw: "https://github.com/owneronly", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseRemoteURL(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestTokenFromEnv(t *testing.T) {
	// NITPICK_TOKEN takes precedence over GITHUB_TOKEN.
	t.Setenv("GITHUB_TOKEN", "gh-token")
	t.Setenv("NITPICK_TOKEN", "nit-token")
	got, err := Token()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "nit-token" {
		t.Errorf("got %q, want nit-token", got)
	}
}
