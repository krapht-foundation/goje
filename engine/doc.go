// Package engine provides the platform-independent building blocks for Goje
// games. It defines isometric projection and game-owned playground state;
// the caller drives updates, handles input, and draws the scene. Keeping host
// services outside this package lets games exercise their logic headlessly.
package engine
