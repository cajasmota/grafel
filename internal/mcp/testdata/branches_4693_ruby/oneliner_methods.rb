class StatusController < ApplicationController
  # An endless / one-liner method (Ruby 3.0+) has no separate `end` body, so the
  # clamp must NOT over-clamp at the header line and drop the branchy sibling
  # below. Its own branch is the postfix modifier on the next real method.
  def ping = head(:ok)

  def health_check
    return head :service_unavailable if Maintenance.active?
    render status: 200, json: { ok: true }
  end
end
