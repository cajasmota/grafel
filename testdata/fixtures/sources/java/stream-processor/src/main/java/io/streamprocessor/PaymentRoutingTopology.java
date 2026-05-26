package io.streamprocessor;

import org.apache.kafka.streams.StreamsBuilder;
import org.apache.kafka.streams.kstream.KStream;
import org.apache.kafka.streams.KafkaStreams;
import org.apache.kafka.streams.Topology;

/**
 * Payment routing topology — #1480 fixture.
 *
 * Consumes payments.settled (source topic).
 * Uses .through("payments.normalised") to route records through an intermediate
 * repartition topic before writing final output.
 *
 * Demonstrates the full Streams DSL surface extracted by #1480:
 *   builder.stream(...)      → SUBSCRIBES_TO
 *   kStream.through(...)     → PUBLISHES_TO + SUBSCRIBES_TO  (repartition)
 *   kStream.filter().to(...) → PUBLISHES_TO  (chained sink)
 */
public class PaymentRoutingTopology {

    public Topology buildTopology() {
        StreamsBuilder builder = new StreamsBuilder();

        KStream<String, Payment> payments = builder.stream("payments.settled");

        // Repartition through an intermediate topic.
        KStream<String, Payment> repartitioned = payments
                .through("payments.normalised");

        // Route normalised payments to the final output topic.
        repartitioned
                .filter((key, value) -> value != null && value.amount > 0)
                .to("orders.enriched");

        return builder.build();
    }
}
