package main

import (
	"fmt"
	"io"
	"log"
)

// resetPassword меняет пароль пользователя напрямую в файлах данных.
// Используется, когда единственный администратор забыл пароль.
// Служба при этом должна быть остановлена, иначе она перезапишет users.json
// своим состоянием из памяти.
func resetPassword(cfg Config, login, password string) error {
	st, err := NewStore(cfg.DataDir, log.New(io.Discard, "", 0))
	if err != nil {
		return err
	}
	defer st.Close()

	u := st.UserByLogin(login)
	if u == nil {
		return fmt.Errorf("пользователь «%s» не найден", login)
	}
	salt := newSalt()
	hash, err := hashPassword(password, salt)
	if err != nil {
		return err
	}
	st.UpdateUser(u.ID, func(x *User) {
		x.Salt = salt
		x.PassHash = hash
		x.Blocked = false
	})
	st.DeleteUserSessions(u.ID)
	return nil
}
