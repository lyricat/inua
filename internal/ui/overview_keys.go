package ui

import (
	"context"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/lyricat/inua/internal/desktop"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/hypr"
)

// Observe raw keyboard events rather than binding SUPER+every-key. This
// preserves all user chords and handles keys held before Super correctly.
const overviewKeyScript = `
if _G.inuaOverviewListener then _G.inuaOverviewListener:remove();_G.inuaOverviewListener=nil end
if _G.inuaOverviewKeyTimer then _G.inuaOverviewKeyTimer:set_enabled(false) end
local down = {}
local candidate = nil
local navigating = false
_G.inuaOverviewListener = hl.on("input.keyboard.key", function(code, time, state)
 local super = code == 133 or code == 134
 if state == 1 then
  if down[code] then return end
  if super then
   candidate = next(down) == nil and code or nil
  else
   candidate = nil
  end
  down[code] = true
 else
  down[code] = nil
  if super then
   if navigating then
    if not down[133] and not down[134] then
     navigating = false
     hl.dispatch(hl.dsp.event("inua-overview,commit"))
    end
   elseif candidate == code then
    hl.dispatch(hl.dsp.event("inua-overview,toggle"))
   end
   candidate = nil
  end
 end
end)
hl.unbind("SUPER + TAB")
hl.unbind("SUPER + SHIFT + TAB")
hl.bind("SUPER + TAB", function()
 candidate = nil
 navigating = true
 hl.dispatch(hl.dsp.event("inua-overview,next"))
end, { description = "Inua overview next workspace" })
hl.bind("SUPER + SHIFT + TAB", function()
 candidate = nil
 navigating = true
 hl.dispatch(hl.dsp.event("inua-overview,previous"))
end, { description = "Inua overview previous workspace" })
`

// Without raw keyboard events, query Hyprland's held-key state only while
// Super is held. This observes unbound chords too, without competing with
// ordinary SUPER+key bindings. A sub-4ms press/release can escape polling.
// Older Hyprland builds have neither raw key events nor held-key queries.
// Use compositor shadowing for standalone Win and a transparent release only
// for Win+Tab commit. Existing bound Super chords shadow the toggle binding.
const overviewReleaseKeyScript = `
if _G.inuaOverviewListener then _G.inuaOverviewListener:remove();_G.inuaOverviewListener=nil end
if _G.inuaOverviewKeyTimer then _G.inuaOverviewKeyTimer:set_enabled(false) end
local navigating=false
for _,key in ipairs({"SUPER_L","SUPER_R"}) do
 hl.unbind("SUPER + "..key)
 hl.bind("SUPER + "..key,function()
  if not navigating then hl.dispatch(hl.dsp.event("inua-overview,toggle")) end
 end,{release=true,non_consuming=true,description="Inua overview standalone Win"})
 hl.bind("SUPER + "..key,function()
  if navigating then navigating=false;hl.dispatch(hl.dsp.event("inua-overview,commit")) end
 end,{release=true,transparent=true,ignore_mods=true,non_consuming=true,description="Inua overview Win release commit"})
end
hl.unbind("SUPER + TAB")
hl.unbind("SUPER + SHIFT + TAB")
hl.bind("SUPER + TAB",function()
 navigating=true;hl.dispatch(hl.dsp.event("inua-overview,next"))
end,{description="Inua overview next workspace"})
hl.bind("SUPER + SHIFT + TAB",function()
 navigating=true;hl.dispatch(hl.dsp.event("inua-overview,previous"))
end,{description="Inua overview previous workspace"})
`

const overviewNativeKeyScript = `
if type(hl.is_key_down) ~= "function" then
` + overviewReleaseKeyScript + `
else
if _G.inuaOverviewListener then _G.inuaOverviewListener:remove(); _G.inuaOverviewListener=nil end
if _G.inuaOverviewKeyTimer then _G.inuaOverviewKeyTimer:set_enabled(false) end
local candidate=nil
local navigating=false
local function interrupted(own)
 for code=8,255 do
  if code~=own and hl.is_key_down(code) then return true end
 end
 return false
end
_G.inuaOverviewKeyTimer=hl.timer(function()
 if candidate and interrupted(candidate) then candidate=nil;_G.inuaOverviewKeyTimer:set_enabled(false) end
end,{timeout=4,type="repeat"})
_G.inuaOverviewKeyTimer:set_enabled(false)
for _,key in ipairs({"SUPER_L","SUPER_R"}) do
 local own=key=="SUPER_L" and 133 or 134
 hl.unbind("SUPER + "..key)
 hl.bind("SUPER + "..key,function()
  candidate=not interrupted(own) and own or nil
  _G.inuaOverviewKeyTimer:set_enabled(candidate~=nil)
 end,{transparent=true,ignore_mods=true,non_consuming=true,description="Inua overview Win press"})
 hl.bind("SUPER + "..key,function()
  _G.inuaOverviewKeyTimer:set_enabled(false)
  if navigating then
   navigating=false
   hl.dispatch(hl.dsp.event("inua-overview,commit"))
  elseif candidate==own and not interrupted(own) then
   hl.dispatch(hl.dsp.event("inua-overview,toggle"))
  end
  candidate=nil
 end,{release=true,transparent=true,ignore_mods=true,non_consuming=true,description="Inua overview standalone Win"})
end
hl.unbind("SUPER + TAB")
hl.unbind("SUPER + SHIFT + TAB")
hl.bind("SUPER + TAB",function()
 candidate=nil;navigating=true
 hl.dispatch(hl.dsp.event("inua-overview,next"))
end,{description="Inua overview next workspace"})
hl.bind("SUPER + SHIFT + TAB",function()
 candidate=nil;navigating=true
 hl.dispatch(hl.dsp.event("inua-overview,previous"))
end,{description="Inua overview previous workspace"})
end
`

// installOverviewKeys installs the Lua listeners for a Super tap, Super+Tab
// and the number slots, on a Hyprland that runs Lua.
func (s *shellUI) installOverviewKeys() {
	if s.hypr == nil || !s.hyprLua {
		return
	}
	client := s.hypr.Client
	func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		reply, err := client.Request(ctx, "eval "+overviewKeyScript)
		if err != nil {
			log.Printf("overview shortcuts: %v", err)
			return
		}
		if strings.TrimSpace(string(reply)) != "ok" {
			reply, err = client.Request(ctx, "eval "+overviewNativeKeyScript)
			if err != nil || strings.TrimSpace(string(reply)) != "ok" {
				log.Printf("overview native shortcuts: %s %v", reply, err)
			}
		}
		numbers := `
_G.inuaOverviewNumbersOwned=true
for slot=1,10 do
 local n=slot
 hl.unbind("SUPER + code:"..tostring(slot+9))
 hl.bind("SUPER + code:"..tostring(slot+9),function() hl.dispatch(hl.dsp.event("inua-overview,slot:"..tostring(n))) end,{description="Inua overview monitor slot "..tostring(slot)})
end`

		if reply, err := client.Request(ctx, "eval "+numbers); err != nil || strings.TrimSpace(string(reply)) != "ok" {
			log.Printf("overview slots: %s %v", reply, err)
		}
	}()
}

func (s *shellUI) overviewEvent(e hypr.Event) {
	if e.Name != "custom" || !strings.HasPrefix(e.Data, "inua-overview,") {
		return
	}
	gtkx.Post(func() {
		action := strings.TrimPrefix(e.Data, "inua-overview,")
		if strings.HasPrefix(action, "slot:") {
			slot, err := strconv.Atoi(strings.TrimPrefix(action, "slot:"))
			if err != nil {
				return
			}
			order := s.overview.st
			order.MRU = s.cfg.Overview.MRU
			card, ok := order.WorkspaceSlot(s.snap, s.snap.FocusedOutput, slot)
			if !ok {
				return
			}
			if card.New {
				s.createWorkspaceOnMonitor(card.Workspace.Output)
			} else {
				id := card.Workspace.ID
				s.activateOverview(card.Workspace.Output, func(ctx context.Context) error { return s.comp.FocusWorkspace(ctx, id) })
			}
			return
		}
		switch action {
		case "toggle":
			s.showOverview()
		case "next", "previous":
			if !s.overview.st.Open {
				s.showOverview()
			}
			delta := 1
			if strings.HasSuffix(e.Data, ",previous") {
				delta = -1
			}
			s.navigateOverview(delta)
			s.renderOverview()
		case "commit":
			if s.overview.st.Open {
				s.commitOverview()
			}
		}
	})
}

func (s *shellUI) commitOverview() {
	if s.overview.st.Query != "" {
		if res, ok := s.overview.st.SelectedSearchResult(); ok {
			if res.Address != "" {
				s.activateOverview(s.overviewWindowMonitor(res.Address), func(ctx context.Context) error { return s.comp.FocusWindow(ctx, res.Address) })
				return
			}
			if res.Entry != nil {
				s.closeOverview(false)
				_ = desktop.LaunchWith(res.Entry, s.cfg.TerminalCommand())
				return
			}
		}
	}
	o := &s.overview.st
	ws, _, ok := o.Select(o.Selected)
	if !ok {
		return
	}
	newWorkspace := o.Cards[o.Selected].New
	if newWorkspace {
		s.createWorkspaceOnMonitor(ws.Output)
	} else {
		s.activateOverview(ws.Output, func(ctx context.Context) error { return s.comp.FocusWorkspace(ctx, ws.ID) })
	}
}

// Stop observers before process replacement or normal shutdown. Bindings are
// reinstalled by the next shell connection, without reloading the compositor.
func (s *shellUI) pauseOverviewKeys() {
	if s.hypr == nil || !s.hyprLua {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, _ = s.hypr.Client.Request(ctx, `eval if _G.inuaOverviewKeyTimer then _G.inuaOverviewKeyTimer:set_enabled(false) end;if _G.inuaOverviewListener then _G.inuaOverviewListener:remove();_G.inuaOverviewListener=nil end`)
}
