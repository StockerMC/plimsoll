// Package ml implements the model-selection memory recommender from the
// Autopilot paper (Rzadca et al., EuroSys '20), section 3.2.3: a set of
// arg-min models, each a decayed histogram read at the limit that minimizes
// a weighted overrun and underrun cost (Eq. 4 to 7), and a selector that
// picks the model whose recent limits would have cost least (Eq. 8 and 9).
package ml
