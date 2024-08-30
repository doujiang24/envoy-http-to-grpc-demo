package demo

import (
	"encoding/json"
	"fmt"

	"github.com/envoyproxy/envoy/contrib/golang/common/go/api"
	"github.com/golang/protobuf/proto"
	demo "github.com/zlingqu/go-grpc-demo/data"
)

type filter struct {
	api.PassThroughStreamFilter

	callbacks api.FilterCallbackHandler

	respHeader     api.ResponseHeaderMap
	severStreaming bool

	remainBuf []byte
}

func (f *filter) badRequest(err error) api.StatusType {
	body := fmt.Sprintf("bad request: %v", err)
	f.callbacks.DecoderFilterCallbacks().SendLocalReply(400, body, nil, 0, "")
	return api.LocalReply
}

func (f *filter) DecodeHeaders(header api.RequestHeaderMap, endStream bool) api.StatusType {
	if endStream {
		return f.badRequest(fmt.Errorf("no data"))
	}
	header.Set("content-type", "application/grpc")
	header.Del("content-length")

	path := header.Path()
	if path == "/demo.Demo/GetStream" {
		f.severStreaming = true
	}
	// wait all data
	return api.StopAndBuffer
}

func setGrpcHeader(grpcHeader []byte, size int) {
	grpcHeader[4] = byte(size >> 0)
	grpcHeader[3] = byte(size >> 8)
	grpcHeader[2] = byte(size >> 16)
	grpcHeader[1] = byte(size >> 24)
}

func (f *filter) DecodeData(buffer api.BufferInstance, endStream bool) api.StatusType {
	jsonBuf := buffer.Bytes()
	data := &demo.TwoNum{}
	if err := json.Unmarshal(jsonBuf, data); err != nil {
		return f.badRequest(err)
	}

	grpcHeader := make([]byte, 5, 10)
	pbBuf := proto.NewBuffer(grpcHeader)
	if err := pbBuf.Marshal(data); err != nil {
		return f.badRequest(err)
	}

	grpcBuf := pbBuf.Bytes()
	size := len(grpcBuf) - 5

	setGrpcHeader(grpcBuf, size)
	buffer.Set(grpcBuf)

	return api.Continue
}

func (f *filter) badResponse(err error) api.StatusType {
	body := fmt.Sprintf("bad response: %v", err)
	f.callbacks.EncoderFilterCallbacks().SendLocalReply(500, body, nil, 0, "")
	return api.LocalReply
}

func (f *filter) EncodeHeaders(header api.ResponseHeaderMap, endStream bool) api.StatusType {
	contentType, _ := header.Get("content-type")
	if contentType != "application/grpc" {
		return f.badResponse(fmt.Errorf("bad content type: %s", contentType))
	}
	if f.severStreaming {
		header.Set("content-type", "text/event-stream")
		return api.Continue
	} else {
		header.Set("content-type", "application/json")
		f.respHeader = header
		return api.StopAndBuffer
	}
}

func (f *filter) recv(buffer api.BufferInstance) {
	if buffer.Len() == 0 {
		return
	}

	if len(f.remainBuf) == 0 {
		f.remainBuf = buffer.Bytes()
	} else {
		f.remainBuf = append(f.remainBuf, buffer.Bytes()...)
	}
}

func (f *filter) transCoder() ([]byte, error) {
	if len(f.remainBuf) < 5 {
		return nil, nil
	}
	size := int(f.remainBuf[1])<<24 | int(f.remainBuf[2])<<16 | int(f.remainBuf[3])<<8 | int(f.remainBuf[4])
	if len(f.remainBuf) < size+5 {
		return nil, nil
	}
	grpcBuf := f.remainBuf[5 : size+5]
	f.remainBuf = f.remainBuf[size+5:]
	data := &demo.Response{}
	err := proto.Unmarshal(grpcBuf, data)
	if err != nil {
		return nil, err
	}
	jsonBuf, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	return jsonBuf, nil
}

func (f *filter) EncodeData(buffer api.BufferInstance, endStream bool) api.StatusType {
	f.recv(buffer)
	jsonBuf, err := f.transCoder()
	if err != nil {
		return f.badResponse(err)
	}
	if !f.severStreaming {
		f.respHeader.Set("content-length", fmt.Sprintf("%d", len(jsonBuf)))
		buffer.Set(jsonBuf)
		if endStream {
			return api.Continue
		}
		return api.StopAndBuffer
	}

	first := true
	// no reaming message
	for jsonBuf != nil {
		msg := fmt.Sprintf("event: message\\ndata: %s\n\n", jsonBuf)
		if first {
			first = false
			buffer.SetString(msg)
		} else {
			buffer.AppendString(msg)
		}

		jsonBuf, err = f.transCoder()
		if err != nil {
			return f.badResponse(err)
		}
	}
	return api.Continue
}

func (f *filter) EncodeTrailers(trailers api.ResponseTrailerMap) api.StatusType {
	grpcStatus, _ := trailers.Get("grpc-status")
	grpcMessage, _ := trailers.Get("grpc-message")
	if !f.severStreaming {
		if grpcStatus != "0" {
			f.respHeader.Set(":status", "500")
		}
		f.respHeader.Set("grpc-status", grpcStatus)
		f.respHeader.Set("grpc-message", grpcMessage)
	} else {
		// TODO: append data
	}
	return api.Continue
}
