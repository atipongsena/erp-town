package world

import (
	"slices"

	"github.com/atipongsena/erp-town/go/internal/auth"
	pb "github.com/atipongsena/erp-town/go/internal/worldpb/world/v1"
)

// VisibleEntities returns only facts the verified viewer may see in this company.
func VisibleEntities(state State, viewer auth.Claims) []Entity {
	if !slices.Contains(viewer.Companies, state.Company()) || viewer.Subject == "" {
		return nil
	}
	return slices.DeleteFunc(state.Entities(), func(entity Entity) bool { return !visibleTo(entity, viewer) })
}

func visibleTo(entity Entity, viewer auth.Claims) bool {
	switch entity.Visibility.Level {
	case "public":
		return true
	case "roles":
		return slices.ContainsFunc(entity.Visibility.Roles, func(role string) bool { return slices.Contains(viewer.Roles, role) })
	case "private":
		return viewer.Subject != "" && entity.Actor.Kind == "user" && entity.Actor.ID == "user."+viewer.Subject
	default:
		return false
	}
}

func wireKey(key EntityKey) *pb.EntityKey {
	return &pb.EntityKey{Doctype: key.Subject.DocType, Name: key.Subject.Name, Kind: string(key.Kind), Scope: key.Scope}
}

func wireEntity(entity Entity) *pb.Entity {
	return &pb.Entity{
		Key: wireKey(entity.Key), Version: entity.Version, EventType: entity.EventType,
		DataJson: entity.Data, Time: entity.Time, Seq: entity.Seq, EventId: entity.EventID,
		CorrelationId: entity.CorrelationID, ActorId: entity.Actor.ID,
	}
}

func frameFor(state State, viewer auth.Claims, seen map[EntityKey]uint64, base uint64, token string, snapshot bool) (*pb.ServerFrame, map[EntityKey]uint64) {
	frame := &pb.ServerFrame{Kind: pb.FrameKind_FRAME_KIND_PATCH, Company: state.Company(), Version: state.Version(), BaseVersion: base, ResumeToken: token}
	if snapshot {
		frame.Kind = pb.FrameKind_FRAME_KIND_SNAPSHOT
		frame.BaseVersion = 0
	}
	next := make(map[EntityKey]uint64)
	for _, entity := range VisibleEntities(state, viewer) {
		next[entity.Key] = entity.Version
		if snapshot || seen[entity.Key] != entity.Version {
			frame.Upserts = append(frame.Upserts, wireEntity(entity))
		}
	}
	if !snapshot {
		for key := range seen {
			if _, exists := next[key]; !exists {
				frame.Removes = append(frame.Removes, wireKey(key))
			}
		}
		slices.SortFunc(frame.Removes, func(a, b *pb.EntityKey) int {
			return slices.Compare([]string{a.Doctype, a.Name, a.Kind, a.Scope}, []string{b.Doctype, b.Name, b.Kind, b.Scope})
		})
	}
	return frame, next
}
