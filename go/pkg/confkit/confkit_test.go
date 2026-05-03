package confkit_test

import (
	"os"
	"path/filepath"
	"testing"

	"nof0-api/pkg/confkit"
)

func TestResolvePath(t *testing.T) {
	tmp := t.TempDir()
	base := filepath.Join(tmp, "base", "dir")
	absFile := filepath.Join(tmp, "absolute", "path", "file.yaml")
	envHome := filepath.Join(tmp, "env-home")

	tests := []struct {
		name     string
		base     string
		file     string
		expected string
		setupEnv map[string]string
	}{
		{
			name:     "absolute path",
			base:     base,
			file:     absFile,
			expected: absFile,
		},
		{
			name:     "relative path",
			base:     base,
			file:     filepath.Join("config", "file.yaml"),
			expected: filepath.Join(base, "config", "file.yaml"),
		},
		{
			name:     "absolute path with env var",
			base:     base,
			file:     "$TEST_CONF_HOME/config/file.yaml",
			expected: filepath.Join(envHome, "config", "file.yaml"),
			setupEnv: map[string]string{"TEST_CONF_HOME": envHome},
		},
		{
			name:     "relative path with env var",
			base:     base,
			file:     "${TEST_CONF_REL}/file.yaml",
			expected: filepath.Join(base, "testvalue", "file.yaml"),
			setupEnv: map[string]string{"TEST_CONF_REL": "testvalue"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for key, value := range tt.setupEnv {
				t.Setenv(key, value)
			}

			result := confkit.ResolvePath(tt.base, tt.file)
			if result != tt.expected {
				t.Errorf("ResolvePath() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestBaseDir(t *testing.T) {
	tmp := t.TempDir()
	absMain := filepath.Join(tmp, "etc", "config", "app.yaml")
	rootMain := filepath.Join(filepath.VolumeName(tmp)+string(os.PathSeparator), "app.yaml")

	tests := []struct {
		name     string
		mainPath string
		expected string
	}{
		{
			name:     "absolute path",
			mainPath: absMain,
			expected: filepath.Dir(absMain),
		},
		{
			name:     "root path",
			mainPath: rootMain,
			expected: filepath.Dir(rootMain),
		},
		{
			name:     "relative path",
			mainPath: filepath.Join("config", "app.yaml"),
			expected: "config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := confkit.BaseDir(tt.mainPath)
			if result != tt.expected {
				t.Errorf("BaseDir() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestSectionHydrate(t *testing.T) {
	t.Run("empty file", func(t *testing.T) {
		section := &confkit.Section[string]{}
		err := section.Hydrate(t.TempDir(), func(path string) (*string, error) {
			t.Error("loader should not be called for empty file")
			return nil, nil
		})
		if err != nil {
			t.Errorf("Hydrate() with empty file should not error, got: %v", err)
		}
		if section.Value != nil {
			t.Error("Value should remain nil for empty file")
		}
	})

	t.Run("successful hydration", func(t *testing.T) {
		base := t.TempDir()
		section := &confkit.Section[string]{File: "config.yaml"}
		expected := "test value"
		expectedPath := filepath.Join(base, "config.yaml")

		err := section.Hydrate(base, func(path string) (*string, error) {
			if path != expectedPath {
				t.Errorf("loader received path %v, want %v", path, expectedPath)
			}
			return &expected, nil
		})

		if err != nil {
			t.Errorf("Hydrate() error = %v, want nil", err)
		}
		if section.Value == nil || *section.Value != expected {
			t.Errorf("Value = %v, want %v", section.Value, expected)
		}
		if section.File != expectedPath {
			t.Errorf("File = %v, want %v", section.File, expectedPath)
		}
	})
}
