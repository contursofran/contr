package app

import (
	"github.com/egoist/mygo"

	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/chatview"
	"github.com/contursofran/contr/internal/app/ghsource"
	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/orchestrate"
	"github.com/contursofran/contr/internal/app/projectsview"
	"github.com/contursofran/contr/internal/app/questions"
	"github.com/contursofran/contr/internal/app/reviewview"
	"github.com/contursofran/contr/internal/app/settings"
	"github.com/contursofran/contr/internal/app/sidebar"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/app/view"
	"github.com/contursofran/contr/internal/app/viewers"
	"github.com/contursofran/contr/internal/host"
	"github.com/contursofran/contr/internal/projects"
	"github.com/contursofran/contr/internal/statedir"
	"github.com/contursofran/contr/internal/usage"
)

// Attach connects the frame's state to the window. A change of any of them
// redraws the window; Visibility follows the window. Call it once, on the UI
// thread, after the window exists. The returned function ends the watching.
//
// Each feature has one function that makes its views and returns its stop (#570). The calls run
// in this order because a later feature reads the views of an earlier one. The stop runs the
// stops in the order of the old single function: the menu handlers first, so that no menu item
// reaches a stopped view, and the sidebar and its samplers last.
func (f *Frame) Attach(w Window) (stop func()) {
	env := f.attachState(w)
	stopFiles := f.attachFiles(env)
	agentSnap, src, stopAgents := f.attachAgents(w, env)
	stopSidebar := f.attachSidebar(w, env, agentSnap, src)
	f.attachProjects(env)
	stopSettings := f.attachSettings(w, env)
	f.loadSaved(env)
	f.attachQuestions(env, agentSnap, src)
	stopChat := f.attachChat(env, src)
	unwatch := f.attachVisibility(w, src)
	term := f.attachTerminal(w)
	offMenu := f.attachMenu(w, src)
	return func() {
		offMenu()
		stopSettings()
		stopFiles()
		stopChat()
		unwatch()
		term()
		stopAgents()
		stopSidebar()
	}
}

// attachState makes the selection and the toasts, and returns the window's Env for the views:
// one value, not a field per view.
func (f *Frame) attachState(w Window) view.Env {
	f.Selection = state.NewSelection(func() {
		f.syncOpen()
		w.Invalidate()
		f.syncReview()
		f.syncTerminal()
		f.syncChat()
	})
	f.Toasts = state.NewToasts(w.Invalidate)
	return view.Env{Run: view.Runner{Post: w.Update}, Invalidate: w.Invalidate, Quiet: f.deps.RunOnMain,
		Status: f.Toasts.Show, OpenURL: openURL}
}

// attachFiles gives the files, review and session views the Env, and makes the PR pane while the
// viewers show. The stop closes the files, review and session views.
func (f *Frame) attachFiles(env view.Env) (stop func()) {
	f.Review = reviewview.NewReview(env)
	f.Files.Env = env
	f.Session.Env = env
	if f.Session.StateDir == "" {
		f.Session.StateDir = statedir.Dir()
	}
	if viewersEnabled {
		f.PR = &viewers.PRPane{Env: env}
	}
	return func() {
		f.Files.Close()
		f.Review.Close()
		f.Session.Close()
	}
}

// attachAgents starts the agent state through Deps.StartAgents and makes the views that read it:
// the questions card, the close-terminal flow, the worker panel and the outline. agentSnap and
// src are nil without agents. The stop removes the outline, then stops the agent state.
func (f *Frame) attachAgents(w Window, env view.Env) (agentSnap func() agentstate.Snapshot, src agentstate.AgentSource, stop func()) {
	if f.deps.AgentsNotice != "" {
		f.Toasts.Show(f.deps.AgentsNotice)
	}
	gh := &ghsource.GHQueue{} // one limit for both (#568)
	f.WorkerPRs = &ghsource.WorkerPRs{Env: env, GH: gh}
	f.IssueTitles = &ghsource.IssueTitles{Env: env, GH: gh}
	stopAgents := func() {}
	if f.deps.StartAgents != nil {
		src, stopAgents = f.deps.StartAgents(func() { w.Update(f.syncAgents) })
	}
	if src != nil {
		f.agentSnap = src.Snapshot
		f.takeSnapshot()
		agentSnap = f.snapshot
		src = storedSource{AgentSource: src, snap: f.snapshot}
		f.Card = &questions.QuestionsCard{Source: src, Env: env,
			Select: func(address string, orchestrator bool) {
				kind := state.KindWorker
				if orchestrator {
					kind = state.KindOrchestrator
				}
				f.Selection.Select(state.Selected{Kind: kind, ID: address})
			}}
		f.CloseTerm = &CloseTerminal{Stop: src.Close, Env: env, Closed: f.terminalClosed}
		entry := ghsource.WorkerEntry{PRs: f.WorkerPRs, OpenPR: f.paneOpener(), OpenIssue: f.OpenIssue}
		f.Card.Entry = entry
		if viewersEnabled {
			f.Worker = &viewers.WorkerPanel{Entry: entry, Source: src, Selection: f.Selection, Env: env}
			f.PR.Orchestrator, f.PR.Send = f.prOrchestrator, src.Send
		}
	}
	removeOutline := func() {}
	if agentSnap != nil {
		f.Outline = questions.NewOutline(agentSnap, f.Selection, env)
		removeOutline = f.Overlay.Add(questions.LabelOutline, f.Outline.View)
	}
	return agentSnap, src, func() {
		removeOutline()
		stopAgents()
	}
}

// attachSidebar makes the sidebar list, the samplers that it shows (memory and plan usage) and
// the new orchestrator and new terminal flows. The git scan runs on a goroutine and lands through
// Window.Update. The stop closes the list, then the samplers.
func (f *Frame) attachSidebar(w Window, env view.Env, agentSnap func() agentstate.Snapshot, src agentstate.AgentSource) (stop func()) {
	if f.Host == nil {
		f.Host = host.NewSystem(theme.MemBarWidth, w.Invalidate)
	}
	if f.Usage == nil {
		f.Usage = usage.NewFileSampler(w.Invalidate)
	}
	if f.SpacesList == nil {
		f.SpacesList = sidebar.NewSpacesList(agentSnap, f.Selection, env)
	}
	l := f.SpacesList
	l.Host = f.Host
	l.ByKey = func(do func()) { f.keyNav = true; do(); f.keyNav = false }
	l.OnOpen = func() { f.focusTerm = true; w.Invalidate() }
	l.OnPick = l.OnOpen
	l.ModelOf = f.deps.ModelOf
	l.OnGroups = func() { f.Files.Sync() } // the folder of the tree follows the groups (#585)
	l.EffortOf = f.deps.EffortOf
	l.StatusEffort = f.statusEffort
	l.StatusModel = f.statusModel
	l.Titles = f.IssueTitles
	l.Entry = f.agentRowEntry()
	l.Ages = &sidebar.AgeClock{Tick: w.Invalidate}
	l.Plans = func() []kit.Meter { return planMeters(f.planUsage()) }
	if src != nil {
		l.Source = src
		l.OnRetry = src.Reconnect
	}
	l.WatchHead = f.deps.WatchHead
	l.OnError = func(err error) { f.Toasts.Show(err.Error()) }
	l.HoldLaunch = f.deps.Projects.Load != nil
	f.Slots.Spaces = l.View
	if f.deps.PickFolder != nil && f.deps.SpawnOrchestrator != nil {
		f.NewOrch = &NewOrchestrator{Pick: f.deps.PickFolder, Spawn: f.deps.SpawnOrchestrator,
			Spawned: l.SelectWhenShown, Env: env}
		l.OnNew = f.NewOrch.Start
		l.OnNewIn = f.NewOrch.StartIn
	}
	if f.deps.OpenTerminal != nil {
		f.NewTerm = &NewTerminal{Open: f.deps.OpenTerminal, Env: env,
			Select: func(addr string) { f.Selection.Select(state.Selected{Kind: state.KindTerminal, ID: addr}) }}
	}
	return func() {
		f.SpacesList.Close()
		f.Host.Close()
		f.Usage.Close()
	}
}

// attachProjects makes the projects modal. The modal and the sidebar list call each other, so
// they connect here, in one place: the modal reads the list's groups and switches its project;
// the list opens the modal, hides a project through it and tells it of a reorder or an adopt.
func (f *Frame) attachProjects(env view.Env) {
	l := f.SpacesList
	m := &projectsview.ProjectsModal{
		Groups: l.Groups,
		Store:  func() *projects.Store { return f.Projects },
		Apply: func() {
			if f.Projects != nil {
				f.SpacesList.SetProjects(f.Projects)
			}
		},
		PickFolder: f.deps.OpenFolder, Resolve: f.deps.ProjectKey,
		CloneParent: f.deps.CloneParent, PickParent: f.deps.PickCloneParent, CloneRun: f.deps.CloneRun,
		Save: f.deps.Projects.Save, Env: env,
		Toast: f.Toasts.ShowAction,
	}
	f.ProjectsModal = m
	m.Switch = l.Switch
	m.Orchestrated = l.Orchestrated
	l.OnHide = m.Hide
	l.OnProjects = m.Toggle
	l.OnReordered = m.Changed
	l.OnAdopted = m.Changed
}

// attachSettings gives the appearance its window, shows a theme load error and makes the
// settings panel with its update checks. The sidebar opens the panel and shows its update pill.
// The stop ends the checks and the watch of the app activation.
func (f *Frame) attachSettings(w Window, env view.Env) (stop func()) {
	f.lateWin.set(w)
	if f.themeErr != nil {
		f.Toasts.Show(f.themeErr.Error())
		f.themeErr = nil
	}
	f.Settings = &settings.Settings{Updates: f.deps.Updates, Version: settings.Version, CoreVersion: f.coreVersion, Env: env,
		Relaunch: f.deps.Relaunch, OnReady: f.updateReady, Visible: func() bool { return f.Visibility == nil || f.Visibility.Visible() },
		AgentsConfig: f.deps.AgentsConfig, Appearance: f.Appearance, SetAppearance: f.SetAppearance,
		Tint: f.Tint, SetTint: f.SetTint, ResetTint: f.ResetTint, ChatText: f.ChatText, SetChatText: f.SetChatText, RecordInstall: f.recordInstall}
	f.loadUpdated(env)
	f.SpacesList.OnSettings = func() { f.Settings.Toggle() }
	f.SpacesList.UpdateReady = f.Settings.Ready
	stopChecks := f.Settings.StartChecks()
	offActive := mygo.App.OnDidBecomeActive(func() { w.Update(f.Settings.Activated) })
	return func() {
		stopChecks()
		offActive()
	}
}

// loadSaved scans the sidebar for the first time, after the projects modal and the settings
// connect to it, and reads the saved projects and widths off the UI thread.
func (f *Frame) loadSaved(env view.Env) {
	f.SpacesList.Sync()
	f.loadProjects(env)
	f.loadWidths(env)
}

// attachQuestions puts the Chat | Terminal switch and the notch with the questions card above the
// terminal, makes the orchestrate pill of the title bar, starts the first pull request and issue
// title reads and, while the viewers show, makes the board panel. The switch layer comes first, so
// the notch draws over it.
func (f *Frame) attachQuestions(env view.Env, agentSnap func() agentstate.Snapshot, src agentstate.AgentSource) {
	f.Notch = &questions.Notch{Snapshot: agentSnap, Project: func() string { return f.openProject }}
	if src != nil {
		f.Notch.Open = f.Card.Open
		f.Notch.Close = f.Card.Close
		f.Notch.Opened = f.Card.IsOpen
		f.Notch.Panel = f.Card.View
		f.Pill = &orchestrate.Pill{Snapshot: agentSnap, Selected: f.selectedTerminal, Send: src.Send, Env: env}
		corner := &chatview.Corner{Switch: f.chatSwitch, OutlineTop: f.Outline.Top}
		f.Overlay.Add(chatview.LabelSwitchLayer, corner.View)
	}
	f.Overlay.Add(questions.LabelNotchLayer, f.Notch.View)
	f.syncWorkerPRs()
	f.syncIssueTitles()
	if viewersEnabled {
		f.attachBoard(agentSnap, src)
	}
}

// attachVisibility makes Visibility follow the window and tells the samplers, the agent source,
// the terminal and the settings when it changes. The returned function ends the watch.
func (f *Frame) attachVisibility(w Window, src agentstate.AgentSource) (unwatch func()) {
	f.Visibility = state.NewVisibility(func(visible bool) {
		f.SpacesList.Ages.SetVisible(visible)
		if src != nil {
			src.SetVisible(visible) // it does not block
		}
		f.Host.SetVisible(visible)
		f.Usage.SetVisible(visible)
		w.Update(func() {
			if f.conn != nil {
				f.conn.SetVisible(visible)
			}
		})
		w.Update(func() { f.Settings.SetVisible(visible) })
		if visible {
			w.Update(f.syncReview)
		}
	})
	// Visibility starts as visible, so a window that starts hidden is the only change that Watch
	// reports. The samplers start hidden: tell them the window state now.
	f.Host.SetVisible(w.IsVisible())
	f.Usage.SetVisible(w.IsVisible())
	f.SpacesList.Ages.SetVisible(w.IsVisible())
	if src != nil {
		src.SetVisible(w.IsVisible())
	}
	return f.Visibility.Watch(w)
}

// attachMenu sets the menu handlers of the window: Settings and Sidebar always, New Terminal with
// Deps.OpenTerminal and Close Terminal with an agent source. The stop clears all four.
func (f *Frame) attachMenu(w Window, src agentstate.AgentSource) (stop func()) {
	if src != nil {
		setHandler(handlerCloseTerminal, func() { w.Update(f.usedKey(f.closeSelectedTerminal)) })
	}
	if f.deps.OpenTerminal != nil {
		setHandler(handlerNewTerminal, func() { w.Update(f.usedKey(f.newTerminal)) })
	}
	setHandler(handlerSettings, func() { w.Update(f.usedKey(f.Settings.Toggle)) })
	setHandler(handlerSidebar, func() { w.Update(f.usedKey(f.ToggleSidebar)) })
	return func() {
		setHandler(handlerNewTerminal, nil)
		setHandler(handlerSidebar, nil)
		setHandler(handlerCloseTerminal, nil)
		setHandler(handlerSettings, nil)
	}
}
