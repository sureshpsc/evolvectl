package domain

import "time"

// EnsureStages seeds the lifecycle in order.
func (r *RunReport) EnsureStages() {
	if len(r.Stages) == len(StageOrder) {
		return
	}
	existing := map[string]StageRecord{}
	for _, s := range r.Stages {
		existing[s.Name] = s
	}
	r.Stages = make([]StageRecord, 0, len(StageOrder))
	for _, name := range StageOrder {
		if s, ok := existing[name]; ok {
			r.Stages = append(r.Stages, s)
			continue
		}
		r.Stages = append(r.Stages, StageRecord{Name: name, State: StagePending})
	}
}

// MarkStage records a transition.
func (r *RunReport) MarkStage(name, state, reason, skippedBy string, now time.Time) {
	r.EnsureStages()
	for i := range r.Stages {
		if r.Stages[i].Name != name {
			continue
		}
		if r.Stages[i].StartedAt == nil && (state == StageRunning || state == StagePassed || state == StageFailed || state == StageBlocked || state == StageCancelled) {
			t := now
			r.Stages[i].StartedAt = &t
		}
		r.Stages[i].State = state
		r.Stages[i].Reason = reason
		r.Stages[i].SkippedBy = skippedBy
		if state != StageRunning {
			t := now
			r.Stages[i].EndedAt = &t
		}
		return
	}
}
