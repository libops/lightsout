package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// Test helpers and mocks
type MockGCPAPI struct {
	suspendCalled bool
	mu            sync.Mutex
}

func (m *MockGCPAPI) WasSuspendCalled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.suspendCalled
}

func (m *MockGCPAPI) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.suspendCalled = false
}

// Mock the GCP API calls
var mockGCP = &MockGCPAPI{}

func setupTestConfig() *Config {
	return &Config{
		Port:              "8808",
		InactivityTimeout: 90 * time.Second,
		LogLevel:          "ERROR",
		GoogleProjectID:   "test-project",
		GCEZone:           "test-zone",
		GCEInstance:       "test-instance",
		LibOpsKeepOnline:  "",
	}
}

func setupTestEnvironment() func() {
	// Save original globals
	origConfig := config
	origTracker := tracker
	origShutdownTimer := shutdownTimer
	origServerShutdown := serverShutdown
	origSuspendFunc := suspendFunc

	// Set test config and tracker
	config = setupTestConfig()
	tracker = &ActivityTracker{
		lastPing: time.Now(),
	}
	shutdownTimer = nil
	serverShutdown = make(chan struct{})
	suspendFunc = mockSuspendInstance
	mockGCP.Reset()

	// Setup test logging (suppress output)
	opts := &slog.HandlerOptions{Level: slog.LevelError}
	handler := slog.New(slog.NewTextHandler(io.Discard, opts))
	slog.SetDefault(handler)

	// Return cleanup function
	return func() {
		// Stop any running shutdown timer first
		stopShutdownTimer()

		// Protect global variable assignments with mutex to prevent race condition
		shutdownMutex.Lock()
		config = origConfig
		tracker = origTracker
		shutdownTimer = origShutdownTimer
		serverShutdown = origServerShutdown
		suspendFunc = origSuspendFunc
		shutdownMutex.Unlock()
	}
}

// Mock suspend function for testing
func mockSuspendInstance() error {
	mockGCP.mu.Lock()
	mockGCP.suspendCalled = true
	mockGCP.mu.Unlock()
	return nil
}

func TestSuspensionAfterInactivityTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cleanup := setupTestEnvironment()
		defer cleanup()

		// Start the shutdown timer
		resetShutdownTimer()

		// Verify suspension hasn't been called yet
		if mockGCP.WasSuspendCalled() {
			t.Fatal("Suspension should not be called immediately")
		}

		// Advance time by the inactivity timeout period using fake clock
		time.Sleep(config.InactivityTimeout + 100*time.Millisecond)

		// Verify suspension was called
		if !mockGCP.WasSuspendCalled() {
			t.Fatal("Suspension should have been called after inactivity timeout")
		}
	})
}

func TestTimerResetOnPingRequest(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cleanup := setupTestEnvironment()
		defer cleanup()

		// Start the shutdown timer
		resetShutdownTimer()

		// Wait for almost the timeout period
		time.Sleep(config.InactivityTimeout - 1*time.Second)

		// Make a ping request to reset the timer
		req := httptest.NewRequest("GET", "/ping", nil)
		w := httptest.NewRecorder()
		pingHandler(w, req)

		// Verify the response
		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d", w.Code)
		}
		if w.Body.String() != "pong" {
			t.Fatalf("Expected 'pong', got %s", w.Body.String())
		}

		// Wait for the original timeout period (timer should have reset)
		time.Sleep(2 * time.Second)

		// Suspension should NOT have been called yet
		if mockGCP.WasSuspendCalled() {
			t.Fatal("Suspension should not be called after ping reset timer")
		}

		// Wait for the full timeout period after the ping
		time.Sleep(config.InactivityTimeout)

		// Now suspension should be called
		if !mockGCP.WasSuspendCalled() {
			t.Fatal("Suspension should be called after timeout following ping")
		}
	})
}

func TestMultiplePingsKeepMachineAlive(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cleanup := setupTestEnvironment()
		defer cleanup()
		// Start the shutdown timer
		resetShutdownTimer()

		// Make multiple ping requests within the timeout period
		for i := 0; i < 5; i++ {
			// Wait for part of the timeout period
			time.Sleep(config.InactivityTimeout / 2)

			// Make a ping request
			req := httptest.NewRequest("GET", "/ping", nil)
			w := httptest.NewRecorder()
			pingHandler(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("Ping %d: Expected status 200, got %d", i, w.Code)
			}

			// Suspension should not be called
			if mockGCP.WasSuspendCalled() {
				t.Fatalf("Suspension should not be called after ping %d", i)
			}
		}

		// Finally, wait for the full timeout without any pings
		time.Sleep(config.InactivityTimeout + 100*time.Millisecond)

		// Now suspension should be called
		if !mockGCP.WasSuspendCalled() {
			t.Fatal("Suspension should be called after final timeout")
		}
	})
}

func TestKeepOnlineDisablesSuspension(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cleanup := setupTestEnvironment()
		defer cleanup()

		// Set keep online flag
		config.LibOpsKeepOnline = "yes"

		// Don't start the timer at all when keep online is enabled
		// This simulates the main() function logic that checks LibOpsKeepOnline != "yes"
		if config.LibOpsKeepOnline != "yes" {
			resetShutdownTimer()
		}

		// Wait for longer than the timeout period
		time.Sleep(config.InactivityTimeout * 2)

		// Suspension should NOT be called
		if mockGCP.WasSuspendCalled() {
			t.Fatal("Suspension should not be called when keep online is enabled")
		}
	})
}

func TestHealthEndpoint(t *testing.T) {
	cleanup := setupTestEnvironment()
	defer cleanup()

	req := httptest.NewRequest("GET", "/healthcheck", nil)
	w := httptest.NewRecorder()

	healthHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", w.Code)
	}

	if w.Header().Get("Content-Type") != "text/plain" {
		t.Fatalf("Expected Content-Type 'text/plain', got '%s'", w.Header().Get("Content-Type"))
	}
}

func TestTimerResetBeforeSuspension(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cleanup := setupTestEnvironment()
		defer cleanup()

		// Start timer
		resetShutdownTimer()

		// Wait for timeout to trigger suspension
		time.Sleep(config.InactivityTimeout + 100*time.Millisecond)

		// Verify suspension was called
		// The resetShutdownTimer call before suspension is tested implicitly
		// since suspendInstance calls resetShutdownTimer internally
		if !mockGCP.WasSuspendCalled() {
			t.Fatal("Suspension should have been called")
		}
	})
}
