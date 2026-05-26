# Ruby Rails + Sidekiq + Redis fixture.
# Demonstrates: HTTP endpoints, Sidekiq job producer, Sidekiq job consumer, DB access via ActiveRecord.

# config/routes.rb (inline for fixture)
# Rails.application.routes.draw do
#   resources :events, only: [:index, :show, :create, :destroy]
# end

# ── Model ─────────────────────────────────────────────────────────────────────

class Event < ApplicationRecord
  validates :title, presence: true
  validates :status, inclusion: { in: %w[pending processing sent failed] }

  after_create :enqueue_processing

  private

  def enqueue_processing
    EventProcessorWorker.perform_async(id)
  end
end

# ── Controller ────────────────────────────────────────────────────────────────

class EventsController < ApplicationController
  def index
    events = Event.all.order(created_at: :desc)
    render json: events
  end

  def show
    event = Event.find(params[:id])
    render json: event
  rescue ActiveRecord::RecordNotFound
    render json: { error: "not found" }, status: :not_found
  end

  def create
    event = Event.new(event_params)
    if event.save
      render json: event, status: :created
    else
      render json: { errors: event.errors.full_messages }, status: :unprocessable_entity
    end
  end

  def destroy
    event = Event.find(params[:id])
    event.destroy!
    head :no_content
  rescue ActiveRecord::RecordNotFound
    render json: { error: "not found" }, status: :not_found
  end

  private

  def event_params
    params.require(:event).permit(:title, :payload, :status)
  end
end

# ── Workers ───────────────────────────────────────────────────────────────────

class EventProcessorWorker
  include Sidekiq::Worker
  sidekiq_options queue: :events, retry: 3

  def perform(event_id)
    event = Event.find(event_id)
    event.update!(status: "processing")

    # Simulate processing
    result = process_event(event)

    if result
      event.update!(status: "sent")
      NotificationWorker.perform_async(event_id)
    else
      event.update!(status: "failed")
    end
  rescue ActiveRecord::RecordNotFound => e
    logger.error "EventProcessorWorker: event #{event_id} not found: #{e.message}"
  end

  private

  def process_event(event)
    # Example: write a flag to Redis
    redis = Redis.new(url: ENV.fetch("REDIS_URL", "redis://localhost:6379"))
    redis.set("event:#{event.id}:processed", "1", ex: 3600)
    true
  end
end

class NotificationWorker
  include Sidekiq::Worker
  sidekiq_options queue: :notifications, retry: 5

  def perform(event_id)
    event = Event.find(event_id)
    # Send notification (e.g., webhook, email)
    puts "Notifying for event #{event.id}: #{event.title}"
  rescue ActiveRecord::RecordNotFound => e
    logger.error "NotificationWorker: event #{event_id} not found: #{e.message}"
  end
end

class RetryCleanupWorker
  include Sidekiq::Worker
  sidekiq_options queue: :maintenance

  def perform
    failed = Event.where(status: "failed").where("created_at < ?", 1.hour.ago)
    failed.each do |event|
      EventProcessorWorker.perform_async(event.id)
    end
  end
end
