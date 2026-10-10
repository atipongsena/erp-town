package worldpb_test

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	pb "github.com/atipongsena/erp-town/go/internal/worldpb/world/v1"
)

// This fixture is also decoded by the TypeScript client contract test.
const joinGolden = "0a100a05746f6b656e120741434d452d5448"

func TestJoinMatchesTypeScriptGolden(t *testing.T) {
	frame := &pb.ClientFrame{Message: &pb.ClientFrame_Join{Join: &pb.Join{Jwt: "token", Company: "ACME-TH"}}}
	raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(frame)
	require.NoError(t, err)
	require.Equal(t, joinGolden, hex.EncodeToString(raw))
	var decoded pb.ClientFrame
	require.NoError(t, proto.Unmarshal(raw, &decoded))
	require.True(t, proto.Equal(frame, &decoded))
}

func TestServerVersionsAndPayloadRoundTrip(t *testing.T) {
	frame := &pb.ServerFrame{
		Kind: pb.FrameKind_FRAME_KIND_PATCH, Company: "ACME-TH", Version: 1<<54 + 7, BaseVersion: 1<<54 + 6,
		Upserts: []*pb.Entity{{
			Key: &pb.EntityKey{Doctype: "Sales Order", Name: "SO-1", Kind: "document"}, Version: 1<<54 + 7,
			EventType: "erp.sales_order.submitted", DataJson: []byte(`{"status":"Submitted"}`),
		}},
		Removes: []*pb.EntityKey{{Doctype: "Item", Name: "ITEM-1", Kind: "stock", Scope: "WH-1"}}, ResumeToken: "resume",
	}
	raw, err := proto.Marshal(frame)
	require.NoError(t, err)
	var decoded pb.ServerFrame
	require.NoError(t, proto.Unmarshal(raw, &decoded))
	require.True(t, proto.Equal(frame, &decoded), "uint64 versions and exact JSON bytes must survive wire encoding")
	require.Error(t, proto.Unmarshal([]byte{0x0a, 0xff}, &decoded))
}
