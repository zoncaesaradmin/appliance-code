package bootstrap_test

import (
	"errors"
	"testing"

	"appliance-code/server/backend/internal/bootstrap"
	"appliance-code/server/backend/internal/roles"
	"appliance-code/server/backend/internal/storage"
	"appliance-code/server/backend/internal/storage/sqlite"
)

func TestRoleCRUD(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()

	role, err := h.roles.Create(ctx, systemActor(), "custom-reader", []string{roles.PermUsersRead, roles.PermRolesRead})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if role.BuiltIn {
		t.Error("custom role should not be marked built-in")
	}

	if _, err := h.roles.Create(ctx, systemActor(), "bad-role", []string{"not.a.real.permission"}); !errors.Is(err, roles.ErrUnknownPermission) {
		t.Errorf("Create with unknown permission error = %v, want ErrUnknownPermission", err)
	}

	if err := h.roles.Update(ctx, systemActor(), role.ID, []string{roles.PermUsersRead}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	perms, err := sqlite.NewRoleStore(h.db).ListRolePermissions(ctx, role.ID)
	if err != nil {
		t.Fatalf("ListRolePermissions: %v", err)
	}
	if len(perms) != 1 || perms[0] != roles.PermUsersRead {
		t.Errorf("permissions after update = %v, want [%s]", perms, roles.PermUsersRead)
	}

	if err := h.roles.Update(ctx, systemActor(), roles.AdministratorRoleID, []string{roles.PermUsersRead}); !errors.Is(err, roles.ErrBuiltInRole) {
		t.Errorf("Update on built-in role error = %v, want ErrBuiltInRole", err)
	}
	if err := h.roles.Delete(ctx, systemActor(), roles.AdministratorRoleID); !errors.Is(err, roles.ErrBuiltInRole) {
		t.Errorf("Delete on built-in role error = %v, want ErrBuiltInRole", err)
	}

	if err := h.roles.Delete(ctx, systemActor(), role.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := h.roles.Get(ctx, role.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Get after delete error = %v, want ErrNotFound", err)
	}
}

func TestUserEnableUpdateDisplayNameAndUnlock(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()

	if _, err := bootstrap.Init(ctx, h.db, sqlite.NewUserStore(h.db), sqlite.NewRoleStore(h.db), h.users, "admin", "a-very-long-bootstrap-password", "Administrator"); err != nil {
		t.Fatalf("bootstrap.Init: %v", err)
	}
	dev, err := h.users.Create(ctx, systemActor(), "developer", "Dev", "a-very-long-developer-password")
	if err != nil {
		t.Fatalf("creating developer: %v", err)
	}

	if err := h.users.UpdateDisplayName(ctx, systemActor(), dev.ID, "New Display Name"); err != nil {
		t.Fatalf("UpdateDisplayName: %v", err)
	}
	got, err := h.users.Get(ctx, dev.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.DisplayName != "New Display Name" {
		t.Errorf("DisplayName = %q, want %q", got.DisplayName, "New Display Name")
	}

	if err := h.users.Disable(ctx, systemActor(), dev.ID); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if err := h.users.Enable(ctx, systemActor(), dev.ID); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	got, err = h.users.Get(ctx, dev.ID)
	if err != nil {
		t.Fatalf("Get after enable: %v", err)
	}
	if got.State != storage.UserStateActive {
		t.Errorf("state after Enable = %q, want active", got.State)
	}

	// Trigger a lockout via repeated failed logins, then confirm Unlock
	// clears it.
	for i := 0; i < 21; i++ {
		if _, err := h.sessions.Login(ctx, "127.0.0.1", "req", "developer", "wrong-password"); err == nil {
			t.Fatal("expected login failure")
		}
	}
	if _, err := h.sessions.Login(ctx, "127.0.0.1", "req", "developer", "a-very-long-developer-password"); err == nil {
		t.Fatal("account should be locked after repeated failures")
	}
	if err := h.users.Unlock(ctx, systemActor(), "developer"); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if _, err := h.sessions.Login(ctx, "127.0.0.1", "req", "developer", "a-very-long-developer-password"); err != nil {
		t.Errorf("login after unlock should succeed: %v", err)
	}
}
