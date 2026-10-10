package config

import "testing"

func TestFromEnv(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		want    Config
		wantErr bool
	}{
		{
			name: "defaults",
			env:  map[string]string{"DATABASE_BINDING": "todo-db-app"},
			want: Config{Port: 8080, BindingDir: "/bindings/todo-db-app"},
		},
		{
			name: "overrides",
			env:  map[string]string{"PORT": "9090", "SERVICE_BINDING_ROOT": "/var/run/bindings", "DATABASE_BINDING": "todo-db-owner"},
			want: Config{Port: 9090, BindingDir: "/var/run/bindings/todo-db-owner"},
		},
		{name: "binding name missing", env: map[string]string{}, wantErr: true},
		{name: "port not a number", env: map[string]string{"PORT": "http", "DATABASE_BINDING": "todo-db-app"}, wantErr: true},
		{name: "port zero", env: map[string]string{"PORT": "0", "DATABASE_BINDING": "todo-db-app"}, wantErr: true},
		{name: "port too large", env: map[string]string{"PORT": "65536", "DATABASE_BINDING": "todo-db-app"}, wantErr: true},
		{name: "binding name escapes root", env: map[string]string{"DATABASE_BINDING": "../etc"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FromEnv(func(k string) string { return tt.env[k] })
			if (err != nil) != tt.wantErr {
				t.Fatalf("FromEnv error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("FromEnv = %+v, want %+v", got, tt.want)
			}
		})
	}
}
