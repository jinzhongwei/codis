package topom

import (
	"testing"

	"github.com/wandoulabs/codis/pkg/models"
	"github.com/wandoulabs/codis/pkg/proxy"
	"github.com/wandoulabs/codis/pkg/utils/assert"
)

func getSlotMapping(t *Topom, sid int) *models.SlotMapping {
	ctx, err := t.newContext()
	assert.MustNoError(err)
	m, err := ctx.getSlotMapping(sid)
	assert.MustNoError(err)
	assert.Must(m.Id == sid)
	return m
}

func checkSlots(t *Topom, c *proxy.ApiClient) {
	ctx, err := t.newContext()
	assert.MustNoError(err)

	slots1 := ctx.toSlotSlice(ctx.slots)
	assert.Must(len(slots1) == models.MaxSlotNum)

	slots2, err := c.Slots()
	assert.MustNoError(err)
	assert.Must(len(slots2) == models.MaxSlotNum)

	for i := 0; i < len(slots1); i++ {
		a := slots1[i]
		b := slots2[i]
		assert.Must(a.Id == b.Id)
		assert.Must(a.Locked == b.Locked)
		assert.Must(a.BackendAddr == b.BackendAddr)
		assert.Must(a.MigrateFrom == b.MigrateFrom)
	}
}

func TestSlotCreateAction(x *testing.T) {
	t := openTopom()
	defer t.Close()

	const sid = 100
	const gid = 200

	assert.Must(t.SlotCreateAction(sid, gid) != nil)

	g := &models.Group{Id: gid}
	contextUpdateGroup(t, g)
	assert.Must(t.SlotCreateAction(sid, gid) != nil)

	g.Servers = []*models.GroupServer{
		&models.GroupServer{Addr: "server"},
	}
	contextUpdateGroup(t, g)
	assert.MustNoError(t.SlotCreateAction(sid, gid))

	assert.Must(t.SlotCreateAction(sid, gid) != nil)

	m := getSlotMapping(t, sid)
	assert.Must(m.GroupId == 0)
	assert.Must(m.Action.State == models.ActionPending)
	assert.Must(m.Action.Index > 0 && m.Action.TargetId == gid)

	m = &models.SlotMapping{Id: sid, GroupId: gid}
	contextUpdateSlotMapping(t, m)
	assert.Must(t.SlotCreateAction(sid, gid) != nil)
}

func TestSlotRemoveAction(x *testing.T) {
	t := openTopom()
	defer t.Close()

	const sid = 100
	assert.Must(t.SlotRemoveAction(sid) != nil)

	sstates := []string{
		models.ActionNothing,
		models.ActionPending,
		models.ActionPreparing,
		models.ActionPrepared,
		models.ActionMigrating,
		models.ActionFinished,
	}

	m := &models.SlotMapping{Id: sid}
	for _, m.Action.State = range sstates {
		contextUpdateSlotMapping(t, m)
		if m.Action.State == models.ActionPending {
			assert.MustNoError(t.SlotRemoveAction(sid))
		} else {
			assert.Must(t.SlotRemoveAction(sid) != nil)
		}
	}
}

func prepareSlotAction(t *Topom, sid int, must bool) *models.SlotMapping {
	i, err := t.SlotActionPrepare()
	if must {
		assert.MustNoError(err)
		assert.Must(sid == i)
	}
	return getSlotMapping(t, sid)
}

func completeSlotAction(t *Topom, sid int, must bool) *models.SlotMapping {
	err := t.SlotActionComplete(sid)
	if must {
		assert.MustNoError(err)
	}
	return getSlotMapping(t, sid)
}

func TestSlotActionSimple(x *testing.T) {
	t := openTopom()
	defer t.Close()

	const sid = 100
	const gid = 200

	m := &models.SlotMapping{Id: sid}
	m.Action.State = models.ActionPending
	m.Action.TargetId = gid
	contextUpdateSlotMapping(t, m)

	m1 := prepareSlotAction(t, sid, true)
	assert.Must(m1.GroupId == 0)
	assert.Must(m1.Action.State == models.ActionMigrating)

	m2 := completeSlotAction(t, sid, true)
	assert.Must(m2.GroupId == gid)
	assert.Must(m2.Action.State == models.ActionNothing)
}

func TestSlotActionPending(x *testing.T) {
	t := openTopom()
	defer t.Close()

	const sid = 100
	const gid = 200

	reset := func() {
		m := &models.SlotMapping{Id: sid}
		m.Action.State = models.ActionPending
		m.Action.TargetId = gid
		contextUpdateSlotMapping(t, m)
	}

	reset()

	m1 := prepareSlotAction(t, sid, true)
	assert.Must(m1.Action.State == models.ActionMigrating)

	reset()

	p, c := openProxy()
	defer c.Shutdown()

	contextCreateProxy(t, p)

	m2 := prepareSlotAction(t, sid, true)
	assert.Must(m2.Action.State == models.ActionMigrating)
	checkSlots(t, c)

	assert.MustNoError(c.Shutdown())

	reset()

	m3 := prepareSlotAction(t, sid, false)
	assert.Must(m3.Action.State == models.ActionPreparing)
}

func TestSlotActionPreparing(x *testing.T) {
	t := openTopom()
	defer t.Close()

	const sid = 100
	const gid = 200

	reset := func() {
		m := &models.SlotMapping{Id: sid}
		m.Action.State = models.ActionPreparing
		m.Action.TargetId = gid
		contextUpdateSlotMapping(t, m)
	}

	reset()

	m1 := prepareSlotAction(t, sid, true)
	assert.Must(m1.Action.State == models.ActionMigrating)

	reset()

	p1, c1 := openProxy()
	defer c1.Shutdown()

	p2, c2 := openProxy()
	defer c2.Shutdown()

	contextCreateProxy(t, p1)
	contextCreateProxy(t, p2)

	m2 := prepareSlotAction(t, sid, true)
	assert.Must(m2.Action.State == models.ActionMigrating)
	checkSlots(t, c1)
	checkSlots(t, c2)

	assert.MustNoError(c1.Shutdown())

	reset()

	m3 := prepareSlotAction(t, sid, false)
	assert.Must(m3.Action.State == models.ActionPreparing)
	checkSlots(t, c2)
}

func TestSlotActionPrepared(x *testing.T) {
	t := openTopom()
	defer t.Close()

	const gid1, gid2 = 200, 300
	const server1 = "server1:port"
	const server2 = "server2:port"

	g1 := &models.Group{Id: gid1}
	g1.Servers = []*models.GroupServer{
		&models.GroupServer{Addr: server1},
	}
	contextCreateGroup(t, g1)
	g2 := &models.Group{Id: gid2}
	g2.Servers = []*models.GroupServer{
		&models.GroupServer{Addr: server2},
	}
	contextCreateGroup(t, g2)

	const sid = 100

	reset := func() {
		m := &models.SlotMapping{Id: sid, GroupId: gid1}
		m.Action.State = models.ActionPrepared
		m.Action.TargetId = gid2
		contextUpdateSlotMapping(t, m)
	}

	reset()

	m1 := prepareSlotAction(t, sid, true)
	assert.Must(m1.Action.State == models.ActionMigrating)

	reset()

	p1, c1 := openProxy()
	defer c1.Shutdown()

	p2, c2 := openProxy()
	defer c2.Shutdown()

	contextCreateProxy(t, p1)
	contextCreateProxy(t, p2)

	m2 := prepareSlotAction(t, sid, true)
	assert.Must(m2.Action.State == models.ActionMigrating)
	checkSlots(t, c1)
	checkSlots(t, c2)

	assert.MustNoError(c1.Shutdown())

	reset()

	m3 := prepareSlotAction(t, sid, false)
	assert.Must(m3.Action.State == models.ActionPrepared)

	slots, err := c2.Slots()
	assert.MustNoError(err)
	assert.Must(len(slots) == models.MaxSlotNum)

	s := slots[sid]
	assert.Must(s.Locked == false)
	assert.Must(s.BackendAddr == server2 && s.MigrateFrom == server1)
}

func TestSlotActionMigrating(x *testing.T) {
	t := openTopom()
	defer t.Close()

	const gid1, gid2 = 200, 300
	const server1 = "server1:port"
	const server2 = "server2:port"

	g1 := &models.Group{Id: gid1}
	g1.Servers = []*models.GroupServer{
		&models.GroupServer{Addr: server1},
	}
	contextCreateGroup(t, g1)
	g2 := &models.Group{Id: gid2}
	g2.Servers = []*models.GroupServer{
		&models.GroupServer{Addr: server2},
	}
	contextCreateGroup(t, g2)

	const sid = 100

	reset := func() {
		m := &models.SlotMapping{Id: sid, GroupId: gid1}
		m.Action.State = models.ActionMigrating
		m.Action.TargetId = gid2
		contextUpdateSlotMapping(t, m)
	}

	reset()

	m1 := completeSlotAction(t, sid, true)
	assert.Must(m1.GroupId == gid2)
	assert.Must(m1.Action.State == models.ActionNothing)

	reset()

	p1, c1 := openProxy()
	defer c1.Shutdown()

	p2, c2 := openProxy()
	defer c2.Shutdown()

	contextCreateProxy(t, p1)
	contextCreateProxy(t, p2)

	m2 := completeSlotAction(t, sid, true)
	assert.Must(m2.GroupId == gid2)
	assert.Must(m2.Action.State == models.ActionNothing)
	checkSlots(t, c1)
	checkSlots(t, c2)

	assert.MustNoError(c1.Shutdown())

	reset()

	m3 := completeSlotAction(t, sid, false)
	assert.Must(m3.GroupId == gid1)
	assert.Must(m3.Action.TargetId == gid2)
	assert.Must(m3.Action.State == models.ActionFinished)

	slots, err := c2.Slots()
	assert.MustNoError(err)
	assert.Must(len(slots) == models.MaxSlotNum)

	s := slots[sid]
	assert.Must(s.Locked == false)
	assert.Must(s.BackendAddr == server2 && s.MigrateFrom == "")
}

func TestSlotActionFinished(x *testing.T) {
	t := openTopom()
	defer t.Close()

	const gid1, gid2 = 200, 300
	const server1 = "server1:port"
	const server2 = "server2:port"

	g1 := &models.Group{Id: gid1}
	g1.Servers = []*models.GroupServer{
		&models.GroupServer{Addr: server1},
	}
	contextCreateGroup(t, g1)
	g2 := &models.Group{Id: gid2}
	g2.Servers = []*models.GroupServer{
		&models.GroupServer{Addr: server2},
	}
	contextCreateGroup(t, g2)

	const sid = 100

	reset := func() {
		m := &models.SlotMapping{Id: sid, GroupId: gid1}
		m.Action.State = models.ActionFinished
		m.Action.TargetId = gid2
		contextUpdateSlotMapping(t, m)
	}

	reset()

	m1 := completeSlotAction(t, sid, true)
	assert.Must(m1.GroupId == gid2)
	assert.Must(m1.Action.State == models.ActionNothing)

	reset()

	p1, c1 := openProxy()
	defer c1.Shutdown()

	p2, c2 := openProxy()
	defer c2.Shutdown()

	contextCreateProxy(t, p1)
	contextCreateProxy(t, p2)

	m2 := completeSlotAction(t, sid, true)
	assert.Must(m2.GroupId == gid2)
	assert.Must(m2.Action.State == models.ActionNothing)
	checkSlots(t, c1)
	checkSlots(t, c2)

	assert.MustNoError(c1.Shutdown())

	reset()

	m3 := completeSlotAction(t, sid, false)
	assert.Must(m3.GroupId == gid1)
	assert.Must(m3.Action.TargetId == gid2)
	assert.Must(m3.Action.State == models.ActionFinished)

	slots, err := c2.Slots()
	assert.MustNoError(err)
	assert.Must(len(slots) == models.MaxSlotNum)

	s := slots[sid]
	assert.Must(s.Locked == false)
	assert.Must(s.BackendAddr == server2 && s.MigrateFrom == "")
}

func TestSlotsRemapGroup(x *testing.T) {
	t := openTopom()
	defer t.Close()

	m := &models.SlotMapping{Id: 100, GroupId: 200}
	m.Action.State = models.ActionPending

	assert.Must(t.SlotsRemapGroup([]*models.SlotMapping{m}) != nil)

	g := &models.Group{Id: 200, Servers: []*models.GroupServer{
		&models.GroupServer{Addr: "server"},
	}}
	contextCreateGroup(t, g)
	assert.Must(t.SlotsRemapGroup([]*models.SlotMapping{m}) != nil)

	m.Action.State = models.ActionNothing
	assert.MustNoError(t.SlotsRemapGroup([]*models.SlotMapping{m}))
}
