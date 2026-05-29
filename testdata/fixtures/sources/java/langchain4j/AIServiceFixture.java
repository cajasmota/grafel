package io.example.langchain4j;

import dev.langchain4j.service.AiService;
import dev.langchain4j.service.SystemMessage;
import dev.langchain4j.service.UserMessage;
import dev.langchain4j.agent.tool.Tool;
import dev.langchain4j.memory.ChatMemory;
import dev.langchain4j.memory.chat.MessageWindowChatMemory;
import dev.langchain4j.model.chat.ChatLanguageModel;
import dev.langchain4j.rag.content.retriever.EmbeddingStoreContentRetriever;
import java.util.List;

/**
 * Fixture for LangChain4J extraction tests.
 * Proves: chain_composition, tool_use_detection, prompt_template_extraction.
 *
 * Expected extractions:
 *   - SCOPE.Service for CustomerSupportAgent (ai_service)
 *   - SCOPE.Pattern entities for @SystemMessage and @UserMessage templates
 *   - SCOPE.Operation entities for @Tool-annotated methods (getFlights, bookFlight)
 *   - SCOPE.Component for ChatLanguageModel field
 *   - SCOPE.Pattern for EmbeddingStoreContentRetriever (RAG component)
 *   - SCOPE.Component for ChatMemory field
 */

@AiService
interface CustomerSupportAgent {

    @SystemMessage("You are a helpful customer support agent for {companyName}.")
    @UserMessage("Customer query: {query}")
    String answer(String companyName, String query);
}

public class BookingTools {

    @Tool("Get available flights for a date")
    public List<Flight> getFlights(String date) {
        return List.of();
    }

    @Tool
    public BookingResult bookFlight(String flightId) {
        return new BookingResult(flightId);
    }
}

public class SupportBot {
    private ChatLanguageModel model;
    private EmbeddingStoreContentRetriever retriever;
    private MessageWindowChatMemory memory;
}
