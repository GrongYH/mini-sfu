package signal

import (
	"context"
	"encoding/json"
	"fmt"

	"mini-sfu/internal/log"
	"mini-sfu/internal/sfu"

	"github.com/pion/webrtc/v3"
	"github.com/sourcegraph/jsonrpc2"
)

// Join 进入房间的信令
type Join struct {
	Sid   string                    `json:"sid"`
	Offer webrtc.SessionDescription `json:"offer"`
}

// Negotiation 重协商时发送的信令
type Negotiation struct {
	Desc webrtc.SessionDescription `json:"desc"`
}

type JSONSignal struct {
	*sfu.Peer
}

// Trickle message sent when renegotiating the peer connection
type Trickle struct {
	Target    int                     `json:"target"`
	Candidate webrtc.ICECandidateInit `json:"candidate"`
}

func NewJSONSignal(p *sfu.Peer) *JSONSignal {
	return &JSONSignal{p}
}

// Handle 处理各种信令，如join，answer，offer
func (p *JSONSignal) Handle(ctx context.Context, conn *jsonrpc2.Conn, req *jsonrpc2.Request) {
	replyError := func(err error) {
		_ = conn.ReplyWithError(ctx, req.ID, &jsonrpc2.Error{
			Code:    500,
			Message: fmt.Sprintf("%s", err),
		})
	}

	switch req.Method {
	case "join":
		var join Join
		err := json.Unmarshal(*req.Params, &join)
		if err != nil {
			log.Errorf("connect: error parsing offer: %v", err)
			replyError(err)
			break
		}

		p.OnOffer = func(offer *webrtc.SessionDescription) {
			if err := conn.Notify(ctx, "offer", offer); err != nil {
				log.Errorf("error sending offer %s", err)
			}
		}

		p.OnIceCandidate = func(candidate *webrtc.ICECandidateInit, target int) {
			//log.Debugf("send ice to client, %s", candidate.Candidate)
			if err := conn.Notify(ctx, "trickle", Trickle{
				Candidate: *candidate,
				Target:    target,
			}); err != nil {
				log.Errorf("error sending ice candidate %s", err)
			}
		}

		err = p.Join(join.Sid)
		if err != nil {
			replyError(err)
			break
		}

		answer, err := p.Answer(join.Offer)
		if err != nil {
			replyError(err)
			break
		}
		_ = conn.Reply(ctx, req.ID, answer)
	case "offer":
		var negotiation Negotiation
		err := json.Unmarshal(*req.Params, &negotiation)
		if err != nil {
			log.Errorf("connect: error parsing offer: %v", err)
			replyError(err)
			break
		}

		answer, err := p.Answer(negotiation.Desc)
		if err != nil {
			replyError(err)
			break
		}
		_ = conn.Reply(ctx, req.ID, answer)
	case "answer":
		var negotiation Negotiation
		err := json.Unmarshal(*req.Params, &negotiation)
		if err != nil {
			log.Errorf("connect: error parsing offer: %v", err)
			replyError(err)
			break
		}
		//log.Infof("get answer %s", negotiation.Desc.SDP)

		err = p.SetRemoteDescription(negotiation.Desc)
		if err != nil {
			replyError(err)
		}

	case "trickle":
		var trickle Trickle
		err := json.Unmarshal(*req.Params, &trickle)
		if err != nil {
			log.Errorf("connect: error parsing candidate: %v", err)
			replyError(err)
			break
		}
		//log.Infof("trickle candidate %s", trickle.Candidate.Candidate)

		err = p.Trickle(trickle.Candidate, trickle.Target)
		if err != nil {
			replyError(err)
		}
	}

}
