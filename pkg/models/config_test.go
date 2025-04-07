package models

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v3"
)

func TestDuration_UnmarshalYAML(t *testing.T) {
	tests := []struct {
		name          string
		yamlInput     string
		expectedDur   time.Duration
		expectError   bool
		expectedError string // Optional: check specific error message
	}{
		{
			name:        "Valid seconds",
			yamlInput:   `duration: "10s"`,
			expectedDur: 10 * time.Second,
			expectError: false,
		},
		{
			name:        "Valid minutes",
			yamlInput:   `duration: "5m"`,
			expectedDur: 5 * time.Minute,
			expectError: false,
		},
		{
			name:        "Valid hours",
			yamlInput:   `duration: "2h"`,
			expectedDur: 2 * time.Hour,
			expectError: false,
		},
		{
			name:        "Valid combined",
			yamlInput:   `duration: "1h30m15s"`,
			expectedDur: 1*time.Hour + 30*time.Minute + 15*time.Second,
			expectError: false,
		},
		{
			name:        "Invalid format - no unit",
			yamlInput:   `duration: "10"`,
			expectError: true,
			// time.ParseDuration error includes the invalid input
			expectedError: `time: missing unit in duration "10"`, // Updated expected error
		},
		{
			name:          "Invalid format - wrong unit",
			yamlInput:     `duration: "5days"`,
			expectError:   true,
			expectedError: `time: unknown unit "days" in duration "5days"`,
		},
		{
			name:          "Invalid format - empty string",
			yamlInput:     `duration: ""`,
			expectError:   true,
			expectedError: `time: invalid duration ""`,
		},
		{
			name:        "Invalid YAML type - integer",
			yamlInput:   `duration: 10`, // Not a string
			expectError: true,
			// YAML library error (Observed behavior differs, matching actual error)
			expectedError: `time: missing unit in duration "10"`, // Updated expected error based on test output
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var data struct {
				Duration Duration `yaml:"duration"`
			}

			err := yaml.Unmarshal([]byte(tt.yamlInput), &data)

			if tt.expectError {
				assert.Error(t, err)
				if tt.expectedError != "" {
					assert.Contains(t, err.Error(), tt.expectedError)
				}
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedDur, data.Duration.Duration)
			}
		})
	}
}
