package controller

import (
	"context"
	"errors"
	"slices"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/9d4/wadoh-be/pb"
	"github.com/gabriel-vasile/mimetype"
)

var ALLOWED_IMAGE_MIME_TYPES = []string{
	"image/jpeg",
	"image/png",
	"image/gif",
	"image/webp",
	"image/heic",
}

type ControllerServiceServer struct {
	pb.UnimplementedControllerServiceServer
	controller *Controller
}

func NewGRPCController(controller *Controller) *ControllerServiceServer {
	return &ControllerServiceServer{controller: controller}
}

func (c *ControllerServiceServer) Status(ctx context.Context, req *pb.StatusRequest) (*pb.StatusResponse, error) {
	status, err := c.controller.Status(req.Jid)
	if err != nil {
		return nil, err
	}

	return &pb.StatusResponse{Status: status}, nil
}

func (c *ControllerServiceServer) RegisterDevice(req *pb.RegisterDeviceRequest, stream pb.ControllerService_RegisterDeviceServer) error {
	resc := make(chan *pb.RegisterDeviceResponse)
	done := make(chan struct{})

	err := c.controller.RegisterNewDevice(req, resc, done)
	if err != nil {
		return err
	}

	go func() {
		<-stream.Context().Done()
		select {
		case <-done:
			return
		default:
		}
		close(done)
	}()

OUTER:
	for {
		select {
		case <-done:
			close(resc)
			break OUTER
		case res := <-resc:
			if res.Jid != nil || res.LoggedIn != nil {
				stream.Send(res)
				break OUTER
			}

			stream.Send(res)
		}
	}

	return nil
}

func (c *ControllerServiceServer) SendMessage(ctx context.Context, req *pb.SendMessageRequest) (*pb.Empty, error) {
	if err := c.controller.SendMessage(ctx, req); err != nil {
		return nil, err
	}

	return &pb.Empty{}, nil
}

func (c *ControllerServiceServer) SendImageMessage(ctx context.Context, req *pb.SendImageMessageRequest) (*pb.SendImageMessageResponse, error) {
	// Validate mime type of req.Image before sending
	mime := mimetype.Detect(req.Image)
	if !slices.Contains(ALLOWED_IMAGE_MIME_TYPES, mime.String()) {
		return nil, status.Error(codes.InvalidArgument, "unsupported image mime type: "+mime.String())
	}

	if err := c.controller.SendMessageImage(ctx, req, mime.String()); err != nil {
		return nil, err
	}
	return nil, nil
}

func (c *ControllerServiceServer) ReceiveMessage(req *pb.Empty, stream pb.ControllerService_ReceiveMessageServer) error {
	recv, err := c.controller.ReceiveMessage(stream.Context())
	if err != nil {
		return err
	}

	for evt := range recv {
		stream.Send(&pb.EventMessage{
			Jid:       evt.JID,
			From:      evt.From,
			Message:   evt.Message,
			MessageId: evt.MessageID,
			PushName:  evt.PushName,
		})
	}
	return nil
}

func (c *ControllerServiceServer) GetWebhook(ctx context.Context, req *pb.GetWebhookRequest) (*pb.GetWebhookResponse, error) {
	res, err := c.controller.GetWebhook(ctx, req.GetJid())
	if err != nil {
		if errors.Is(err, ErrWebhookNotFound) {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, err
	}
	return res, nil
}

func (c *ControllerServiceServer) SaveWebhook(ctx context.Context, req *pb.SaveWebhookRequest) (*pb.Empty, error) {
	if err := c.controller.SaveWebhook(ctx, req.GetJid(), req.GetUrl()); err != nil {
		return nil, err
	}

	return &pb.Empty{}, nil
}

func (c *ControllerServiceServer) DeleteWebhook(ctx context.Context, req *pb.DeleteWebhookRequest) (*pb.Empty, error) {
	if err := c.controller.DeleteWebhook(ctx, req.GetJid()); err != nil {
		return nil, err
	}
	return &pb.Empty{}, nil
}
