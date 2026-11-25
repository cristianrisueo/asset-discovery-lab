package store

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"

	"google.golang.org/protobuf/proto"

	"github.com/cristianrisueo/asset-discovery-lab/datacore/internal/model"
	adlv1 "github.com/cristianrisueo/asset-discovery-lab/gen/go/adl/v1"
)

// Dataset is the seed of spec §5.3, before versions and timestamps are set.
type Dataset struct {
	Groups []*adlv1.Group
	Users  []*adlv1.User
	Assets []*adlv1.Asset
}

// NewDataset builds the deterministic seed dataset (spec §5.3). The same seed
// value and counts always give the same dataset. The map's example is fixed:
// LT-0042 is a Windows 11 laptop in Alicante owned by U-0117, "Ana Ruiz" of
// group G-03 "Ventas".
func NewDataset(seed uint64, c model.SeedCounts) Dataset {
	r := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	var d Dataset

	for i, id := range c.GroupIDs() {
		name := fmt.Sprintf("Grupo %02d", i+1)
		if i < len(model.GroupNames) {
			name = model.GroupNames[i]
		}
		d.Groups = append(d.Groups, &adlv1.Group{Id: id, Name: name})
	}

	groupIDs := c.GroupIDs()
	for _, id := range c.UserIDs() {
		u := &adlv1.User{
			Id:          id,
			DisplayName: model.RandomName(r),
			Email:       strings.ToLower(id) + "@acme.example",
			GroupId:     groupIDs[r.IntN(len(groupIDs))],
		}
		if id == model.ExampleUserID {
			u.DisplayName = model.ExampleUserName
			if c.Groups >= 3 {
				u.GroupId = model.ExampleGroupID
			}
		}
		d.Users = append(d.Users, u)
	}

	userIDs := c.UserIDs()
	for _, id := range c.AssetIDs() {
		kind := model.KindForID(id)
		a := &adlv1.Asset{
			Id:       id,
			Hostname: strings.ToLower(id) + ".acme.local",
			Kind:     kind,
			Os:       model.PickWeighted(r, model.OSPool(id)),
			Site:     model.Sites[r.IntN(len(model.Sites))],
		}
		if kind != adlv1.AssetKind_ASSET_KIND_SERVER && r.IntN(100) < 90 {
			a.OwnerUserId = userIDs[r.IntN(len(userIDs))]
		}
		if id == model.ExampleAssetID {
			a.Os, a.Site = model.ExampleOS, model.ExampleSite
			if c.Users >= 117 {
				a.OwnerUserId = model.ExampleUserID
			}
		}
		d.Assets = append(d.Assets, a)
	}
	return d
}

// SeedBatchSize is the number of entities per seed transaction.
const SeedBatchSize = 500

// Seed inserts the dataset when the seed has not completed yet (spec §7.4):
// groups, then users, then assets, in batches of transactions, with one outbox
// row per entity. It is safe to call on every start.
func (s *Store) Seed(ctx context.Context, seed uint64, c model.SeedCounts) (inserted bool, err error) {
	done, err := s.SeedComplete(ctx)
	if err != nil || done {
		return false, err
	}
	d := NewDataset(seed, c)
	groups := make([]proto.Message, len(d.Groups))
	groupIDs := make([]string, len(d.Groups))
	for i, g := range d.Groups {
		groups[i], groupIDs[i] = g, g.GetId()
	}
	users := make([]proto.Message, len(d.Users))
	userIDs := make([]string, len(d.Users))
	for i, u := range d.Users {
		users[i], userIDs[i] = u, u.GetId()
	}
	assets := make([]proto.Message, len(d.Assets))
	assetIDs := make([]string, len(d.Assets))
	for i, a := range d.Assets {
		assets[i], assetIDs[i] = a, a.GetId()
	}
	for _, part := range []struct {
		t    model.Type
		msgs []proto.Message
		ids  []string
	}{{model.TypeGroup, groups, groupIDs}, {model.TypeUser, users, userIDs}, {model.TypeAsset, assets, assetIDs}} {
		for i := 0; i < len(part.msgs); i += SeedBatchSize {
			j := min(i+SeedBatchSize, len(part.msgs))
			if err := s.insertSeedBatch(ctx, part.t, part.msgs[i:j], part.ids[i:j]); err != nil {
				return false, err
			}
		}
	}
	return true, s.markSeedComplete(ctx)
}
