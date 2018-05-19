package implementation

import (
	// stdlib
	"context"
	"strings"

	// external
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/satori/go.uuid"

	// project
	"github.com/basvanbeek/opencensus-gokit-example/device"
	"github.com/basvanbeek/opencensus-gokit-example/frontend"
	"github.com/basvanbeek/opencensus-gokit-example/qr"
)

// service implements frontend.Service
type service struct {
	devClient device.Service
	qrClient  qr.Service
	logger    log.Logger
}

// NewService creates and returns a new Frontend service instance
func NewService(devClient device.Service, qrClient qr.Service, logger log.Logger) frontend.Service {
	return &service{
		devClient: devClient,
		qrClient:  qrClient,
		logger:    logger,
	}
}

// Unlockdevice returns a new session for allowing device to check-in participants.
func (s *service) UnlockDevice(ctx context.Context, eventID, deviceID uuid.UUID, unlockCode string) (*frontend.Session, error) {
	var (
		logger = log.With(s.logger, "method", "UnlockDevice")
	)

	if eventID == uuid.Nil {
		level.Warn(logger).Log("err", frontend.ErrRequireEventID)
		return nil, frontend.ErrRequireEventID
	}
	if deviceID == uuid.Nil {
		level.Warn(logger).Log("err", frontend.ErrRequireDeviceID)
		return nil, frontend.ErrRequireDeviceID
	}

	unlockCode = strings.Trim(unlockCode, "\r\n\t ")

	if unlockCode == "" {
		level.Warn(logger).Log("err", frontend.ErrRequireUnlockCode)
		return nil, frontend.ErrRequireUnlockCode
	}

	session, err := s.devClient.Unlock(ctx, eventID, deviceID, unlockCode)
	if err != nil {
		return nil, err
	}

	return &frontend.Session{
		EventID:       eventID,
		EventCaption:  session.EventCaption,
		DeviceID:      deviceID,
		DeviceCaption: session.DeviceCaption,
		Token:         "TOKEN",
	}, nil
}

// Generate returns a new QR code device unlock image based on the provided details.
func (s *service) GenerateQR(ctx context.Context, eventID, deviceID uuid.UUID, unlockCode string) ([]byte, error) {
	var (
		logger = log.With(s.logger, "method", "GenerateQR")
	)

	if eventID == uuid.Nil {
		level.Warn(logger).Log("err", frontend.ErrRequireEventID)
		return nil, frontend.ErrRequireEventID
	}
	if deviceID == uuid.Nil {
		level.Warn(logger).Log("err", frontend.ErrRequireDeviceID)
		return nil, frontend.ErrRequireDeviceID
	}

	unlockCode = strings.Trim(unlockCode, "\r\n\t ")

	if unlockCode == "" {
		level.Warn(logger).Log("err", frontend.ErrRequireUnlockCode)
		return nil, frontend.ErrRequireUnlockCode
	}

	return s.qrClient.Generate(
		ctx, eventID.String()+":"+deviceID.String()+":"+unlockCode, qr.LevelM, 256,
	)
}
