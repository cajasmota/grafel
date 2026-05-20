// Synthetic Zig fixture — exercises common std lib patterns
// used in real Zig HTTP server code (inspired by zap/httpz frameworks).
// License: MIT (synthetic)

const std = @import("std");
const mem = std.mem;
const fmt = std.fmt;
const ascii = std.ascii;
const json = std.json;
const math = std.math;
const ArrayList = std.ArrayList;
const StringHashMap = std.StringHashMap;
const Allocator = std.mem.Allocator;

pub const Router = struct {
    routes: StringHashMap(Handler),
    allocator: Allocator,

    pub fn init(allocator: Allocator) Router {
        return Router{
            .routes = StringHashMap(Handler).init(allocator),
            .allocator = allocator,
        };
    }

    pub fn deinit(self: *Router) void {
        self.routes.deinit();
    }

    pub fn get(self: *Router, path: []const u8, handler: Handler) !void {
        try self.routes.put(path, handler);
    }

    pub fn post(self: *Router, path: []const u8, handler: Handler) !void {
        try self.routes.put(path, handler);
    }

    pub fn dispatch(self: *Router, req: *Request) !Response {
        const entry = self.routes.get(req.path) orelse return Response.notFound();
        return entry(req);
    }
};

pub const Request = struct {
    method: []const u8,
    path: []const u8,
    body: []const u8,
    headers: StringHashMap([]const u8),

    pub fn param(self: *const Request, key: []const u8) ?[]const u8 {
        return self.headers.get(key);
    }

    pub fn parseBody(self: *const Request, comptime T: type, allocator: Allocator) !T {
        const parsed = try json.parseFromSlice(T, allocator, self.body, .{});
        return parsed.value;
    }
};

pub const Response = struct {
    status: u16,
    body: []const u8,
    content_type: []const u8 = "application/json",

    pub fn ok(body: []const u8) Response {
        return .{ .status = 200, .body = body };
    }

    pub fn notFound() Response {
        return .{ .status = 404, .body = "not found" };
    }

    pub fn badRequest(body: []const u8) Response {
        return .{ .status = 400, .body = body };
    }

    pub fn write(self: *const Response, writer: anytype) !void {
        try writer.print("HTTP/1.1 {d} OK\r\n", .{self.status});
        try writer.print("Content-Type: {s}\r\n", .{self.content_type});
        try writer.print("Content-Length: {d}\r\n\r\n", .{self.body.len});
        try writer.writeAll(self.body);
    }
};

pub const Handler = *const fn (*Request) Response;

pub fn parseQueryString(allocator: Allocator, query: []const u8) !StringHashMap([]const u8) {
    var map = StringHashMap([]const u8).init(allocator);
    var it = mem.splitScalar(u8, query, '&');
    while (it.next()) |pair| {
        const eq = mem.indexOfScalar(u8, pair, '=') orelse continue;
        const key = pair[0..eq];
        const val = pair[eq + 1 ..];
        const trimmed_key = mem.trim(u8, key, " ");
        const trimmed_val = mem.trim(u8, val, " ");
        try map.put(trimmed_key, trimmed_val);
    }
    return map;
}

pub fn parseInt(s: []const u8) !i64 {
    return fmt.parseInt(i64, s, 10);
}

pub fn formatError(allocator: Allocator, comptime template: []const u8, args: anytype) ![]u8 {
    return fmt.allocPrint(allocator, template, args);
}

pub fn isSafeHeader(name: []const u8) bool {
    if (mem.startsWith(u8, name, "X-")) return true;
    if (ascii.eqlIgnoreCase(name, "content-type")) return true;
    if (ascii.eqlIgnoreCase(name, "accept")) return true;
    if (ascii.eqlIgnoreCase(name, "authorization")) return true;
    return false;
}

pub fn clampBodyLen(len: usize, max_len: usize) usize {
    return math.clamp(len, 0, max_len);
}

pub fn buildJsonResponse(allocator: Allocator, data: anytype) ![]u8 {
    var list = ArrayList(u8).init(allocator);
    defer list.deinit();
    try json.stringify(data, .{}, list.writer());
    return list.toOwnedSlice();
}

pub fn validatePath(path: []const u8) bool {
    if (path.len == 0) return false;
    if (!mem.startsWith(u8, path, "/")) return false;
    for (path) |c| {
        if (!ascii.isPrint(c)) return false;
    }
    return true;
}

pub fn splitPath(allocator: Allocator, path: []const u8) ![][]const u8 {
    var parts = ArrayList([]const u8).init(allocator);
    var it = mem.tokenizeScalar(u8, path, '/');
    while (it.next()) |part| {
        try parts.append(part);
    }
    return parts.toOwnedSlice();
}

test "parseQueryString" {
    const testing = std.testing;
    var arena = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena.deinit();
    const alloc = arena.allocator();

    const map = try parseQueryString(alloc, "foo=bar&baz=qux");
    try testing.expectEqualStrings("bar", map.get("foo").?);
    try testing.expectEqualStrings("qux", map.get("baz").?);
}

test "parseInt" {
    const testing = std.testing;
    try testing.expectEqual(@as(i64, 42), try parseInt("42"));
    try testing.expectEqual(@as(i64, -7), try parseInt("-7"));
}
