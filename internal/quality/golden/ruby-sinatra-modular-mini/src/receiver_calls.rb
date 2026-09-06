# Decoy file for #6917's forbidden rows. NOTHING here is a Sinatra DSL call.
#
# `(?m)` turns `^\s*` from a start-of-TEXT anchor into a start-of-LINE anchor,
# which means indented text is reachable by these patterns for the first time.
# Every construct below is indented and Sinatra-shaped, and every one of them
# must stay out of the graph:
#
#   * a `#` comment opens its line, so `^\s*get` cannot reach past it;
#   * `server.helpers` / `server.configure` / `server.before` / `server.after`
#     are method calls on ANOTHER receiver, and `^\s*helpers` requires the
#     keyword itself to open the line.
class ServerBuilder
  # get '/never-registered' do
  # configure :never_registered do

  def build(server)
    server.helpers Whatever
    server.configure :production do
      server.before do
        server.after do
          server.error 500 do
            nil
          end
        end
      end
    end
    server
  end
end
