package app

// ghQueue limits the gh runs of the window: at most [maxGH] go at once, the others wait in
// order. The frame makes one and gives it to [WorkerPRs] and [IssueTitles], so the limit holds
// for both together (#568). Use it on the UI thread.
type ghQueue struct {
	active  int      // gh runs going
	waiting []func() // gh runs that wait for a free place
}

// Do runs read off the UI thread through env when a place is free, or later in turn. The
// function that read returns runs on the UI thread.
func (q *ghQueue) Do(env Env, read func() func()) {
	start := func() {
		q.active++
		env.Do(func() func() {
			done := read()
			return func() {
				q.active--
				done()
				if len(q.waiting) > 0 && q.active < maxGH {
					next := q.waiting[0]
					q.waiting = q.waiting[1:]
					next()
				}
			}
		})
	}
	if q.active >= maxGH {
		q.waiting = append(q.waiting, start)
		return
	}
	start()
}
