// Sample Zig HTTP server — golden fixture source.
const std = @import("std");
const net = std.net;
const http = std.http;

const User = struct {
    id: u32,
    name: []const u8,
    email: []const u8,
};

const UserStore = struct {
    allocator: std.mem.Allocator,
    users: std.ArrayList(User),
    next_id: u32,

    pub fn init(allocator: std.mem.Allocator) UserStore {
        return .{
            .allocator = allocator,
            .users = std.ArrayList(User).init(allocator),
            .next_id = 1,
        };
    }

    pub fn deinit(self: *UserStore) void {
        self.users.deinit();
    }

    pub fn findById(self: *UserStore, id: u32) ?User {
        for (self.users.items) |user| {
            if (user.id == id) return user;
        }
        return null;
    }

    pub fn create(self: *UserStore, name: []const u8, email: []const u8) !User {
        const user = User{ .id = self.next_id, .name = name, .email = email };
        self.next_id += 1;
        try self.users.append(user);
        return user;
    }

    pub fn delete(self: *UserStore, id: u32) bool {
        const before = self.users.items.len;
        var i: usize = 0;
        while (i < self.users.items.len) {
            if (self.users.items[i].id == id) {
                _ = self.users.orderedRemove(i);
            } else {
                i += 1;
            }
        }
        return self.users.items.len < before;
    }
};

fn handleHealth(response: *http.Server.Response) !void {
    response.status = .ok;
    try response.headers.append("Content-Type", "application/json");
    try response.send();
    try response.writeAll("{\"status\":\"ok\"}");
    try response.finish();
}

pub fn main() !void {
    var gpa = std.heap.GeneralPurposeAllocator(.{}){};
    defer _ = gpa.deinit();
    const allocator = gpa.allocator();

    var store = UserStore.init(allocator);
    defer store.deinit();

    _ = try store.create("Alice", "alice@example.com");
    _ = handleHealth;

    std.debug.print("Server ready\n", .{});
}
