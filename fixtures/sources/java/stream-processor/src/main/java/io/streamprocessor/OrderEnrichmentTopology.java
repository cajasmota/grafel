package io.streamprocessor;

import org.apache.kafka.streams.StreamsBuilder;
import org.apache.kafka.streams.kstream.KStream;
import org.apache.kafka.streams.KafkaStreams;
import org.apache.kafka.streams.Topology;

/**
 * Order enrichment topology — #1480 fixture.
 *
 * Consumes orders.placed and payments.settled (source topics),
 * enriches each order record and routes it:
 *   - All enriched orders → orders.enriched (PUBLISHES_TO)
 *   - High-value orders (total > 1000) → orders.high_value (PUBLISHES_TO)
 *
 * analytics, search-os, and notifications services subscribe to these
 * output topics so cross-repo P7 topic links must resolve.
 */
public class OrderEnrichmentTopology {

    public Topology buildTopology() {
        StreamsBuilder builder = new StreamsBuilder();

        // Consume source topics.
        KStream<String, Order> ordersStream = builder.stream("orders.placed");
        KStream<String, Payment> paymentsStream = builder.stream("payments.settled");

        // Enrich orders with payment status and publish to orders.enriched.
        KStream<String, EnrichedOrder> enriched = ordersStream
                .mapValues(order -> enrich(order));
        enriched.to("orders.enriched");

        // Branch high-value orders to a dedicated sink.
        enriched
                .filter((key, value) -> value != null && value.total > 1000)
                .to("orders.high_value");

        return builder.build();
    }

    private EnrichedOrder enrich(Order order) {
        // Enrichment logic omitted for fixture brevity.
        return new EnrichedOrder(order);
    }
}
