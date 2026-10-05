package server

import (
	"sync"

	"github.com/cruisinme30/spelunk/daemon/internal/query"
)

// rememberedPlans is how many recent searches' plans are kept, so a
// preview can highlight every term of the query that produced the result.
// Older results still preview, highlighting only their own match.
const rememberedPlans = 32

// planMemory keeps the most recent plans by ID.
type planMemory struct {
	mu    sync.Mutex
	next  int
	plans map[int]*query.Plan
}

// remember stores plan and returns its ID, forgetting the oldest plan.
func (m *planMemory) remember(plan *query.Plan) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.plans == nil {
		m.plans = map[int]*query.Plan{}
	}
	m.next++
	m.plans[m.next] = plan
	delete(m.plans, m.next-rememberedPlans)
	return m.next
}

// recall returns the plan with id, or nil if it was forgotten.
func (m *planMemory) recall(id int) *query.Plan {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.plans[id]
}
