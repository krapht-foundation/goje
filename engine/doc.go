// Package engine provides the platform-independent building blocks for Goje
// games. Start a Runtime to enter and step a scene without a graphics host.
// The host schedules updates and translates keyboard events into Input values;
// the runtime owns scene transitions and cleanup. Isometric projects ground
// coordinates and elevation without depending on a rendering backend.
package engine
