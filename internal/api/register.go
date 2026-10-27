package api

import "github.com/danielgtaylor/huma/v2"

func (a *API) register(api huma.API) {
	a.registerAuth(api)
	a.registerReachability(api)
	a.registerOverview(api)
	a.registerKodi(api)
	a.registerArt(api)
	a.registerLibraries(api)
	a.registerDevices(api)
	a.registerRooms(api)
	a.registerSkill(api)
	a.registerRequests(api)
	a.registerMatch(api)
	a.registerSettings(api)
	a.registerConfigIO(api)
	a.registerEvents(api)
	a.registerClientErrors(api)
}
