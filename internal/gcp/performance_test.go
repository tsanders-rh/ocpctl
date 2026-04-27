package gcp

import (
	"context"
	"strings"
	"testing"
	"time"
)

// BenchmarkLabelCache compares cached vs uncached label sanitization
func BenchmarkLabelCache(b *testing.B) {
	sanitizer := func(s string) string {
		// Simulate expensive sanitization
		result := strings.ToLower(s)
		result = strings.ReplaceAll(result, " ", "-")
		result = strings.ReplaceAll(result, "_", "-")
		if len(result) > 63 {
			result = result[:63]
		}
		return strings.TrimRight(result, "-")
	}

	testLabels := []string{
		"my-cluster-123",
		"test-production-env",
		"my-cluster-123", // Duplicate for cache hit
		"dev_test_cluster",
		"test-production-env", // Duplicate
		"another-cluster-name-with-many-characters-that-needs-truncation",
	}

	b.Run("Without Cache", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			for _, label := range testLabels {
				_ = sanitizer(label)
			}
		}
	})

	b.Run("With Cache", func(b *testing.B) {
		cache := NewLabelCache()
		for i := 0; i < b.N; i++ {
			for _, label := range testLabels {
				_ = cache.Get(label, sanitizer)
			}
		}
	})
}

// BenchmarkArgsBuilder compares direct slice building vs ArgsBuilder
func BenchmarkArgsBuilder(b *testing.B) {
	b.Run("Direct Append", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			args := []string{"gcloud", "compute", "instances", "create"}
			args = append(args, "--project", "test-project")
			args = append(args, "--zone", "us-central1-a")
			args = append(args, "--machine-type", "e2-medium")
			if true {
				args = append(args, "--labels", "env=test")
			}
			_ = args
		}
	})

	b.Run("ArgsBuilder", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			args := NewArgsBuilder(10).
				Add("gcloud", "compute", "instances", "create").
				AddKeyValue("--project", "test-project").
				AddKeyValue("--zone", "us-central1-a").
				AddKeyValue("--machine-type", "e2-medium").
				AddIf(true, "--labels", "env=test").
				Build()
			_ = args
		}
	})
}

// BenchmarkStringBuilderPool tests pooled vs unpooled string building
func BenchmarkStringBuilderPool(b *testing.B) {
	b.Run("Without Pool", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			var result strings.Builder
			result.WriteString("cluster-")
			result.WriteString("name-")
			result.WriteString("test-")
			result.WriteString("123")
			_ = result.String()
		}
	})

	b.Run("With Pool", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			sb := GetStringBuilder()
			sb.WriteString("cluster-")
			sb.WriteString("name-")
			sb.WriteString("test-")
			sb.WriteString("123")
			_ = sb.String()
			PutStringBuilder(sb)
		}
	})
}

// BenchmarkParallelExecutor tests parallel vs sequential command execution
func BenchmarkParallelExecutor(b *testing.B) {
	ctx := context.Background()

	commands := []CommandSpec{
		{Name: "echo", Args: []string{"test1"}, Key: "cmd1"},
		{Name: "echo", Args: []string{"test2"}, Key: "cmd2"},
		{Name: "echo", Args: []string{"test3"}, Key: "cmd3"},
		{Name: "echo", Args: []string{"test4"}, Key: "cmd4"},
	}

	b.Run("Sequential", func(b *testing.B) {
		pool := NewCommandPool(1, 5*time.Second)
		for i := 0; i < b.N; i++ {
			for _, cmd := range commands {
				_, _ = pool.Execute(ctx, cmd.Name, cmd.Args...)
			}
		}
	})

	b.Run("Parallel", func(b *testing.B) {
		executor := NewParallelExecutor(4, 5*time.Second)
		for i := 0; i < b.N; i++ {
			_ = executor.ExecuteAll(ctx, commands)
		}
	})
}

// TestLabelCache tests label caching functionality
func TestLabelCache(t *testing.T) {
	cache := NewLabelCache()
	sanitizer := func(s string) string {
		return strings.ToLower(s)
	}

	// First call should compute
	result1 := cache.Get("TEST", sanitizer)
	if result1 != "test" {
		t.Errorf("Expected 'test', got '%s'", result1)
	}

	// Second call should hit cache
	result2 := cache.Get("TEST", sanitizer)
	if result2 != "test" {
		t.Errorf("Expected cached 'test', got '%s'", result2)
	}

	// Verify cache size
	if cache.Size() != 1 {
		t.Errorf("Expected cache size 1, got %d", cache.Size())
	}

	// Clear and verify
	cache.Clear()
	if cache.Size() != 0 {
		t.Errorf("Expected cache size 0 after clear, got %d", cache.Size())
	}
}

// TestCommandPool tests concurrent command execution
func TestCommandPool(t *testing.T) {
	ctx := context.Background()
	pool := NewCommandPool(2, 5*time.Second)

	output, err := pool.Execute(ctx, "echo", "test")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !strings.Contains(string(output), "test") {
		t.Errorf("Expected output to contain 'test', got: %s", output)
	}
}

// TestParallelExecutor tests parallel command execution
func TestParallelExecutor(t *testing.T) {
	ctx := context.Background()
	executor := NewParallelExecutor(4, 5*time.Second)

	commands := []CommandSpec{
		{Name: "echo", Args: []string{"test1"}, Key: "cmd1"},
		{Name: "echo", Args: []string{"test2"}, Key: "cmd2"},
	}

	results := executor.ExecuteAll(ctx, commands)

	if len(results) != 2 {
		t.Errorf("Expected 2 results, got %d", len(results))
	}

	for key, result := range results {
		if result.Err != nil {
			t.Errorf("Command %s failed: %v", key, result.Err)
		}
	}
}

// TestArgsBuilder tests argument building
func TestArgsBuilder(t *testing.T) {
	args := NewArgsBuilder(10).
		Add("gcloud", "compute").
		AddKeyValue("--project", "test").
		AddKeyValue("--zone", ""). // Should skip empty
		AddIf(true, "--verbose").
		AddIf(false, "--quiet"). // Should skip
		Build()

	expected := []string{"gcloud", "compute", "--project", "test", "--verbose"}
	if len(args) != len(expected) {
		t.Errorf("Expected %d args, got %d", len(expected), len(args))
	}

	for i, arg := range expected {
		if args[i] != arg {
			t.Errorf("Arg %d: expected '%s', got '%s'", i, arg, args[i])
		}
	}
}

// TestStringBuilderPool tests pooled string builders
func TestStringBuilderPool(t *testing.T) {
	sb := GetStringBuilder()
	defer PutStringBuilder(sb)

	sb.WriteString("hello")
	sb.WriteByte(' ')
	sb.WriteString("world")

	result := sb.String()
	if result != "hello world" {
		t.Errorf("Expected 'hello world', got '%s'", result)
	}

	if sb.Len() != 11 {
		t.Errorf("Expected length 11, got %d", sb.Len())
	}
}
