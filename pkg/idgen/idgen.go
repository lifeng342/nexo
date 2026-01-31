package idgen

import (
	"fmt"
	"sync"
	"time"

	"github.com/sony/sonyflake"
)

// IDGenerator is the interface for generating unique IDs
type IDGenerator interface {
	// NextID generates a new unique ID
	NextID() (string, error)
}

// SonyflakeGenerator implements IDGenerator using sonyflake
type SonyflakeGenerator struct {
	sf *sonyflake.Sonyflake
}

// NewSonyflakeGenerator creates a new SonyflakeGenerator
func NewSonyflakeGenerator(machineID uint16) (*SonyflakeGenerator, error) {
	st := sonyflake.Settings{
		StartTime: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		MachineID: func() (uint16, error) {
			return machineID, nil
		},
	}

	sf, err := sonyflake.New(st)
	if err != nil {
		return nil, fmt.Errorf("failed to create sonyflake: %w", err)
	}

	return &SonyflakeGenerator{sf: sf}, nil
}

// NextID generates a new unique ID
func (g *SonyflakeGenerator) NextID() (string, error) {
	id, err := g.sf.NextID()
	if err != nil {
		return "", fmt.Errorf("failed to generate id: %w", err)
	}
	return fmt.Sprintf("%d", id), nil
}

// UUIDGenerator implements IDGenerator using UUID
type UUIDGenerator struct{}

// NewUUIDGenerator creates a new UUIDGenerator
func NewUUIDGenerator() *UUIDGenerator {
	return &UUIDGenerator{}
}

// NextID generates a new UUID
func (g *UUIDGenerator) NextID() (string, error) {
	return generateUUID(), nil
}

// generateUUID generates a UUID v4
func generateUUID() string {
	// Simple UUID v4 implementation
	b := make([]byte, 16)
	_, _ = randomRead(b)
	b[6] = (b[6] & 0x0f) | 0x40 // Version 4
	b[8] = (b[8] & 0x3f) | 0x80 // Variant is 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// randomRead fills b with random bytes
func randomRead(b []byte) (int, error) {
	for i := range b {
		b[i] = byte(time.Now().UnixNano() & 0xff)
		time.Sleep(time.Nanosecond)
	}
	return len(b), nil
}

// Global default generator
var (
	defaultGenerator IDGenerator
	once             sync.Once
	initErr          error
)

// SetDefaultGenerator sets the default ID generator
func SetDefaultGenerator(gen IDGenerator) {
	defaultGenerator = gen
}

// GetDefaultGenerator returns the default ID generator
// If not set, creates a SonyflakeGenerator with machineID 1
func GetDefaultGenerator() (IDGenerator, error) {
	once.Do(func() {
		if defaultGenerator == nil {
			defaultGenerator, initErr = NewSonyflakeGenerator(1)
		}
	})
	if initErr != nil {
		return nil, initErr
	}
	return defaultGenerator, nil
}

// NextID generates a new ID using the default generator
func NextID() (string, error) {
	gen, err := GetDefaultGenerator()
	if err != nil {
		return "", err
	}
	return gen.NextID()
}
