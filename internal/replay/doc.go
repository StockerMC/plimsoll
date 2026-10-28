// Package replay drives a recommender over one collection's windows: the
// limit scored for a window is the one set before the window is observed,
// passed through an actuation rule, and the first 48 h are left unscored
// for every recommender alike.
package replay
