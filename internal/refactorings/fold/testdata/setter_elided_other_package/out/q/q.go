package q

import np "example.com/m/p"

var users = []np.User{np.WithEmail(np.User{ID: 1}, "a@x"), {ID: 2}}

var byName = map[string]np.User{"b": np.WithEmail(np.User{}, "b@x")}
