package resolve

import "regexp"

// swiftDynamicPatterns are per-language patterns for Swift.
// Registered via init() into dynamicPatternsByLang.
//
// Swift dynamic-pattern catalog (issue #44). The Swift extractor
// (internal/extractors/swift/swift.go) emits CALLS edges whose ToID
// is the bare trailing method identifier extracted from navigation
// expressions — e.g. `publisher.sink(...)` → ToID="sink",
// `url.appendingPathComponent("users")` → ToID="appendingPathComponent".
// When the receiver is an external framework type (Combine Publisher,
// Foundation URL/URLSession, SwiftUI View) no in-tree entity exists
// for the callee, so the resolver cannot bind it. These stubs are
// statically unresolvable because:
//  1. The Combine / Foundation / SwiftUI types are imported from the
//     Apple SDK — they are never indexed as in-tree entities.
//  2. The extractor emits only the leaf name; without the receiver
//     type the resolver cannot distinguish `publisher.sink` from a
//     user-defined method also named `sink` on a domain type.
//
// The safer-bias rule (#94) is preserved by the per-language gate
// (lang=="swift"): identical names common in other ecosystems
// (`store`, `sink`, `map`, `filter`, `receive`) do NOT fire here for
// Go, Python, Ruby, etc.
//
// Combine publisher operators (the most common unresolved category).
// Apple Combine framework — AnyPublisher, Publisher, Subscriber, etc.
// Each operator appears as a bare leaf name on a publisher chain.
var swiftDynamicPatterns = []*regexp.Regexp{
	// Combine subscription lifecycle.
	regexp.MustCompile(`^sink$`),                // publisher.sink(receiveValue:)
	regexp.MustCompile(`^store$`),               // cancellable.store(in:)
	regexp.MustCompile(`^cancel$`),              // AnyCancellable.cancel()
	regexp.MustCompile(`^eraseToAnyPublisher$`), // publisher.eraseToAnyPublisher()
	// Combine upstream operators.
	regexp.MustCompile(`^receive$`),     // publisher.receive(on:)
	regexp.MustCompile(`^subscribe$`),   // publisher.subscribe(_:)
	regexp.MustCompile(`^connect$`),     // ConnectablePublisher.connect()
	regexp.MustCompile(`^autoconnect$`), // ConnectablePublisher.autoconnect()
	regexp.MustCompile(`^share$`),       // publisher.share()
	regexp.MustCompile(`^multicast$`),   // publisher.multicast(_:)
	// Combine transformation operators.
	regexp.MustCompile(`^mapError$`),       // publisher.mapError(_:)
	regexp.MustCompile(`^flatMap$`),        // publisher.flatMap(_:)
	regexp.MustCompile(`^compactMap$`),     // publisher.compactMap(_:)
	regexp.MustCompile(`^tryMap$`),         // publisher.tryMap(_:)
	regexp.MustCompile(`^tryFlatMap$`),     // publisher.tryFlatMap(_:)
	regexp.MustCompile(`^tryCompactMap$`),  // publisher.tryCompactMap(_:)
	regexp.MustCompile(`^tryFilter$`),      // publisher.tryFilter(_:)
	regexp.MustCompile(`^decode$`),         // publisher.decode(type:decoder:)
	regexp.MustCompile(`^encode$`),         // publisher.encode(encoder:)
	regexp.MustCompile(`^scan$`),           // publisher.scan(_:_:)
	regexp.MustCompile(`^tryScan$`),        // publisher.tryScan(_:_:)
	regexp.MustCompile(`^reduce$`),         // publisher.reduce(_:_:)
	regexp.MustCompile(`^tryReduce$`),      // publisher.tryReduce(_:_:)
	regexp.MustCompile(`^collect$`),        // publisher.collect() / .collect(_:)
	regexp.MustCompile(`^replaceNil$`),     // publisher.replaceNil(with:)
	regexp.MustCompile(`^replaceEmpty$`),   // publisher.replaceEmpty(with:)
	regexp.MustCompile(`^replaceError$`),   // publisher.replaceError(with:)
	regexp.MustCompile(`^setFailureType$`), // publisher.setFailureType(to:)
	// Combine filtering operators.
	regexp.MustCompile(`^removeDuplicates$`), // publisher.removeDuplicates()
	regexp.MustCompile(`^debounce$`),         // publisher.debounce(for:scheduler:)
	regexp.MustCompile(`^throttle$`),         // publisher.throttle(for:scheduler:latest:)
	regexp.MustCompile(`^timeout$`),          // publisher.timeout(_:scheduler:)
	regexp.MustCompile(`^retry$`),            // publisher.retry(_:)
	regexp.MustCompile(`^catchError$`),       // publisher.catch(_:)
	regexp.MustCompile(`^tryCatch$`),         // publisher.tryCatch(_:)
	regexp.MustCompile(`^first$`),            // publisher.first() / .first(where:)
	regexp.MustCompile(`^tryFirst$`),         // publisher.tryFirst(where:)
	regexp.MustCompile(`^last$`),             // publisher.last() / .last(where:)
	regexp.MustCompile(`^tryLast$`),          // publisher.tryLast(where:)
	regexp.MustCompile(`^ignoreOutput$`),     // publisher.ignoreOutput()
	regexp.MustCompile(`^prefix$`),           // publisher.prefix(_:)
	regexp.MustCompile(`^dropFirst$`),        // publisher.dropFirst(_:)
	regexp.MustCompile(`^drop$`),             // publisher.drop(untilOutputFrom:)
	regexp.MustCompile(`^makeConnectable$`),  // publisher.makeConnectable()
	// Combine combining operators.
	regexp.MustCompile(`^zip$`),            // Publishers.Zip / publisher.zip(_:)
	regexp.MustCompile(`^combineLatest$`),  // publisher.combineLatest(_:)
	regexp.MustCompile(`^merge$`),          // publisher.merge(with:)
	regexp.MustCompile(`^switchToLatest$`), // publisher.switchToLatest()
	regexp.MustCompile(`^prepend$`),        // publisher.prepend(_:)
	regexp.MustCompile(`^append$`),         // publisher.append(_:)
	// Combine side-effect operators.
	regexp.MustCompile(`^handleEvents$`),      // publisher.handleEvents(...)
	regexp.MustCompile(`^breakpoint$`),        // publisher.breakpoint()
	regexp.MustCompile(`^breakpointOnError$`), // publisher.breakpointOnError()
	regexp.MustCompile(`^print$`),             // publisher.print(_:)
	regexp.MustCompile(`^measureInterval$`),   // publisher.measureInterval(using:)
	// Combine subject send / value methods.
	regexp.MustCompile(`^send$`),           // subject.send(_:)
	regexp.MustCompile(`^sendCompletion$`), // subject.send(completion:)
	// Combine assign operator.
	regexp.MustCompile(`^assign$`), // publisher.assign(to:on:)
	// Foundation / URLSession + URL methods that appear as bare
	// leaf names from navigation chains on external framework types.
	// These are Apple SDK methods — no in-tree entity is indexed.
	regexp.MustCompile(`^dataTaskPublisher$`),         // URLSession.shared.dataTaskPublisher(for:)
	regexp.MustCompile(`^dataTask$`),                  // URLSession.dataTask(with:)
	regexp.MustCompile(`^uploadTask$`),                // URLSession.uploadTask(with:from:)
	regexp.MustCompile(`^downloadTask$`),              // URLSession.downloadTask(with:)
	regexp.MustCompile(`^webSocketTask$`),             // URLSession.webSocketTask(with:)
	regexp.MustCompile(`^appendingPathComponent$`),    // URL.appendingPathComponent(_:)
	regexp.MustCompile(`^appendingPathExtension$`),    // URL.appendingPathExtension(_:)
	regexp.MustCompile(`^deletingLastPathComponent$`), // URL.deletingLastPathComponent()
	regexp.MustCompile(`^deletingPathExtension$`),     // URL.deletingPathExtension()
	regexp.MustCompile(`^resolvingSymlinksInPath$`),   // URL.resolvingSymlinksInPath()
	regexp.MustCompile(`^standardized$`),              // URL.standardized
	regexp.MustCompile(`^absoluteURL$`),               // URL.absoluteURL
	regexp.MustCompile(`^setValue$`),                  // URLRequest.setValue(_:forHTTPHeaderField:)
	regexp.MustCompile(`^addValue$`),                  // URLRequest.addValue(_:forHTTPHeaderField:)
	// SwiftUI view modifier chain methods. SwiftUI views use a
	// modifier-chaining pattern: `Text(...).font(.headline).padding()`.
	// The Swift tree-sitter extractor extracts each modifier as a bare
	// CALLS edge with the modifier name as ToID. SwiftUI modifiers are
	// framework methods on `View` (an external protocol from SwiftUI
	// module) — no in-tree entity can be indexed for them.
	regexp.MustCompile(`^padding$`),                            // .padding() / .padding(_:)
	regexp.MustCompile(`^frame$`),                              // .frame(width:height:) etc.
	regexp.MustCompile(`^background$`),                         // .background(_:)
	regexp.MustCompile(`^foregroundColor$`),                    // .foregroundColor(_:) (deprecated but common)
	regexp.MustCompile(`^foregroundStyle$`),                    // .foregroundStyle(_:)
	regexp.MustCompile(`^font$`),                               // .font(_:)
	regexp.MustCompile(`^cornerRadius$`),                       // .cornerRadius(_:)
	regexp.MustCompile(`^clipShape$`),                          // .clipShape(_:)
	regexp.MustCompile(`^overlay$`),                            // .overlay(_:)
	regexp.MustCompile(`^shadow$`),                             // .shadow(color:radius:x:y:)
	regexp.MustCompile(`^opacity$`),                            // .opacity(_:)
	regexp.MustCompile(`^scaleEffect$`),                        // .scaleEffect(_:)
	regexp.MustCompile(`^rotationEffect$`),                     // .rotationEffect(_:)
	regexp.MustCompile(`^offset$`),                             // .offset(x:y:)
	regexp.MustCompile(`^position$`),                           // .position(x:y:)
	regexp.MustCompile(`^blur$`),                               // .blur(radius:)
	regexp.MustCompile(`^saturation$`),                         // .saturation(_:)
	regexp.MustCompile(`^brightness$`),                         // .brightness(_:)
	regexp.MustCompile(`^contrast$`),                           // .contrast(_:)
	regexp.MustCompile(`^colorInvert$`),                        // .colorInvert()
	regexp.MustCompile(`^grayscale$`),                          // .grayscale(_:)
	regexp.MustCompile(`^animation$`),                          // .animation(_:)
	regexp.MustCompile(`^transition$`),                         // .transition(_:)
	regexp.MustCompile(`^zIndex$`),                             // .zIndex(_:)
	regexp.MustCompile(`^fixedSize$`),                          // .fixedSize()
	regexp.MustCompile(`^layoutPriority$`),                     // .layoutPriority(_:)
	regexp.MustCompile(`^navigationTitle$`),                    // .navigationTitle(_:)
	regexp.MustCompile(`^navigationBarTitleDisplayMode$`),      // .navigationBarTitleDisplayMode(_:)
	regexp.MustCompile(`^navigationBarHidden$`),                // .navigationBarHidden(_:)
	regexp.MustCompile(`^navigationBarBackButtonHidden$`),      // .navigationBarBackButtonHidden(_:)
	regexp.MustCompile(`^toolbar$`),                            // .toolbar { ... }
	regexp.MustCompile(`^toolbarBackground$`),                  // .toolbarBackground(_:for:)
	regexp.MustCompile(`^toolbarColorScheme$`),                 // .toolbarColorScheme(_:for:)
	regexp.MustCompile(`^searchable$`),                         // .searchable(text:)
	regexp.MustCompile(`^onAppear$`),                           // .onAppear { ... }
	regexp.MustCompile(`^onDisappear$`),                        // .onDisappear { ... }
	regexp.MustCompile(`^onTapGesture$`),                       // .onTapGesture { ... }
	regexp.MustCompile(`^onLongPressGesture$`),                 // .onLongPressGesture { ... }
	regexp.MustCompile(`^onSubmit$`),                           // .onSubmit { ... }
	regexp.MustCompile(`^onChange$`),                           // .onChange(of:) { ... }
	regexp.MustCompile(`^onReceive$`),                          // .onReceive(_:) { ... }
	regexp.MustCompile(`^onDelete$`),                           // .onDelete(perform:)
	regexp.MustCompile(`^onMove$`),                             // .onMove(perform:)
	regexp.MustCompile(`^task$`),                               // .task { ... } (async task modifier)
	regexp.MustCompile(`^refreshable$`),                        // .refreshable { ... }
	regexp.MustCompile(`^swipeActions$`),                       // .swipeActions { ... }
	regexp.MustCompile(`^contextMenu$`),                        // .contextMenu { ... }
	regexp.MustCompile(`^sheet$`),                              // .sheet(isPresented:) { ... }
	regexp.MustCompile(`^fullScreenCover$`),                    // .fullScreenCover(isPresented:) { ... }
	regexp.MustCompile(`^popover$`),                            // .popover(isPresented:) { ... }
	regexp.MustCompile(`^alert$`),                              // .alert(_:isPresented:) { ... }
	regexp.MustCompile(`^confirmationDialog$`),                 // .confirmationDialog(_:isPresented:) { ... }
	regexp.MustCompile(`^fileImporter$`),                       // .fileImporter(isPresented:)
	regexp.MustCompile(`^fileExporter$`),                       // .fileExporter(isPresented:)
	regexp.MustCompile(`^listStyle$`),                          // .listStyle(_:)
	regexp.MustCompile(`^listRowInsets$`),                      // .listRowInsets(_:)
	regexp.MustCompile(`^listRowBackground$`),                  // .listRowBackground(_:)
	regexp.MustCompile(`^listRowSeparator$`),                   // .listRowSeparator(_:)
	regexp.MustCompile(`^navigationDestination$`),              // .navigationDestination(for:) { ... }
	regexp.MustCompile(`^tabItem$`),                            // .tabItem { ... }
	regexp.MustCompile(`^badge$`),                              // .badge(_:)
	regexp.MustCompile(`^tag$`),                                // .tag(_:)
	regexp.MustCompile(`^id$`),                                 // .id(_:) (identity modifier, not entity ID)
	regexp.MustCompile(`^equatable$`),                          // .equatable() (perf modifier)
	regexp.MustCompile(`^drawingGroup$`),                       // .drawingGroup()
	regexp.MustCompile(`^compositingGroup$`),                   // .compositingGroup()
	regexp.MustCompile(`^contentShape$`),                       // .contentShape(_:)
	regexp.MustCompile(`^allowsHitTesting$`),                   // .allowsHitTesting(_:)
	regexp.MustCompile(`^accessibilityLabel$`),                 // .accessibilityLabel(_:)
	regexp.MustCompile(`^accessibilityHint$`),                  // .accessibilityHint(_:)
	regexp.MustCompile(`^accessibilityValue$`),                 // .accessibilityValue(_:)
	regexp.MustCompile(`^accessibilityHidden$`),                // .accessibilityHidden(_:)
	regexp.MustCompile(`^help$`),                               // .help(_:) (accessibility tooltip)
	regexp.MustCompile(`^disabled$`),                           // .disabled(_:)
	regexp.MustCompile(`^hidden$`),                             // .hidden()
	regexp.MustCompile(`^redacted$`),                           // .redacted(reason:)
	regexp.MustCompile(`^unredacted$`),                         // .unredacted()
	regexp.MustCompile(`^privacySensitive$`),                   // .privacySensitive()
	regexp.MustCompile(`^environment$`),                        // .environment(_:_:)
	regexp.MustCompile(`^environmentObject$`),                  // .environmentObject(_:)
	regexp.MustCompile(`^transformEnvironment$`),               // .transformEnvironment(_:transform:)
	regexp.MustCompile(`^preferenceKey$`),                      // .preference(key:value:)
	regexp.MustCompile(`^onPreferenceChange$`),                 // .onPreferenceChange(_:) { ... }
	regexp.MustCompile(`^transformPreference$`),                // .transformPreference(_:_:)
	regexp.MustCompile(`^anchorPreference$`),                   // .anchorPreference(key:value:)
	regexp.MustCompile(`^overlayPreferenceValue$`),             // .overlayPreferenceValue(_:) { ... }
	regexp.MustCompile(`^backgroundPreferenceValue$`),          // .backgroundPreferenceValue(_:) { ... }
	regexp.MustCompile(`^coordinateSpace$`),                    // .coordinateSpace(name:)
	regexp.MustCompile(`^matchedGeometryEffect$`),              // .matchedGeometryEffect(id:in:)
	regexp.MustCompile(`^edgesIgnoringSafeArea$`),              // .edgesIgnoringSafeArea(_:)
	regexp.MustCompile(`^ignoresSafeArea$`),                    // .ignoresSafeArea(_:regions:)
	regexp.MustCompile(`^safeAreaInset$`),                      // .safeAreaInset(edge:content:)
	regexp.MustCompile(`^clipped$`),                            // .clipped()
	regexp.MustCompile(`^mask$`),                               // .mask(_:)
	regexp.MustCompile(`^border$`),                             // .border(_:width:)
	regexp.MustCompile(`^imageScale$`),                         // .imageScale(_:)
	regexp.MustCompile(`^resizable$`),                          // Image.resizable()
	regexp.MustCompile(`^interpolation$`),                      // Image.interpolation(_:)
	regexp.MustCompile(`^antialiased$`),                        // Image.antialiased(_:)
	regexp.MustCompile(`^renderingMode$`),                      // Image.renderingMode(_:)
	regexp.MustCompile(`^symbolRenderingMode$`),                // Image.symbolRenderingMode(_:)
	regexp.MustCompile(`^symbolVariant$`),                      // .symbolVariant(_:)
	regexp.MustCompile(`^textCase$`),                           // .textCase(_:)
	regexp.MustCompile(`^textContentType$`),                    // .textContentType(_:)
	regexp.MustCompile(`^autocapitalization$`),                 // .autocapitalization(_:)
	regexp.MustCompile(`^autocorrectionDisabled$`),             // .autocorrectionDisabled(_:)
	regexp.MustCompile(`^keyboardType$`),                       // .keyboardType(_:)
	regexp.MustCompile(`^submitLabel$`),                        // .submitLabel(_:)
	regexp.MustCompile(`^lineLimit$`),                          // .lineLimit(_:)
	regexp.MustCompile(`^lineSpacing$`),                        // .lineSpacing(_:)
	regexp.MustCompile(`^multilineTextAlignment$`),             // .multilineTextAlignment(_:)
	regexp.MustCompile(`^truncationMode$`),                     // .truncationMode(_:)
	regexp.MustCompile(`^minimumScaleFactor$`),                 // .minimumScaleFactor(_:)
	regexp.MustCompile(`^allowsTightening$`),                   // .allowsTightening(_:)
	regexp.MustCompile(`^flipsForRightToLeftLayoutDirection$`), // .flipsForRightToLeftLayoutDirection(_:)
	// UIKit methods that appear as bare leaf names when invoked on
	// external UIKit types (UIButton, UIView, UIViewController, etc.).
	regexp.MustCompile(`^addTarget$`),                        // UIButton.addTarget(_:action:for:)
	regexp.MustCompile(`^removeTarget$`),                     // UIButton.removeTarget(_:action:for:)
	regexp.MustCompile(`^addGestureRecognizer$`),             // UIView.addGestureRecognizer(_:)
	regexp.MustCompile(`^removeGestureRecognizer$`),          // UIView.removeGestureRecognizer(_:)
	regexp.MustCompile(`^addSubview$`),                       // UIView.addSubview(_:)
	regexp.MustCompile(`^removeFromSuperview$`),              // UIView.removeFromSuperview()
	regexp.MustCompile(`^insertSubview$`),                    // UIView.insertSubview(_:at:)
	regexp.MustCompile(`^bringSubviewToFront$`),              // UIView.bringSubviewToFront(_:)
	regexp.MustCompile(`^sendSubviewToBack$`),                // UIView.sendSubviewToBack(_:)
	regexp.MustCompile(`^setNeedsLayout$`),                   // UIView.setNeedsLayout()
	regexp.MustCompile(`^setNeedsDisplay$`),                  // UIView.setNeedsDisplay()
	regexp.MustCompile(`^layoutIfNeeded$`),                   // UIView.layoutIfNeeded()
	regexp.MustCompile(`^layoutSubviews$`),                   // UIView.layoutSubviews() override
	regexp.MustCompile(`^sizeToFit$`),                        // UIView.sizeToFit()
	regexp.MustCompile(`^sizeThatFits$`),                     // UIView.sizeThatFits(_:)
	regexp.MustCompile(`^setNeedsUpdateConstraints$`),        // UIView.setNeedsUpdateConstraints()
	regexp.MustCompile(`^updateConstraints$`),                // UIView.updateConstraints() override
	regexp.MustCompile(`^updateConstraintsIfNeeded$`),        // UIView.updateConstraintsIfNeeded()
	regexp.MustCompile(`^activate$`),                         // NSLayoutConstraint.activate(_:)
	regexp.MustCompile(`^deactivate$`),                       // NSLayoutConstraint.deactivate(_:)
	regexp.MustCompile(`^addConstraint$`),                    // UIView.addConstraint(_:)
	regexp.MustCompile(`^removeConstraint$`),                 // UIView.removeConstraint(_:)
	regexp.MustCompile(`^addConstraints$`),                   // UIView.addConstraints(_:)
	regexp.MustCompile(`^removeConstraints$`),                // UIView.removeConstraints(_:)
	regexp.MustCompile(`^present$`),                          // UIViewController.present(_:animated:)
	regexp.MustCompile(`^dismiss$`),                          // UIViewController.dismiss(animated:)
	regexp.MustCompile(`^pushViewController$`),               // UINavigationController.pushViewController(_:animated:)
	regexp.MustCompile(`^popViewController$`),                // UINavigationController.popViewController(animated:)
	regexp.MustCompile(`^popToRootViewController$`),          // UINavigationController.popToRootViewController(animated:)
	regexp.MustCompile(`^performSegue$`),                     // UIViewController.performSegue(withIdentifier:sender:)
	regexp.MustCompile(`^reloadData$`),                       // UITableView/UICollectionView.reloadData()
	regexp.MustCompile(`^reloadRows$`),                       // UITableView.reloadRows(at:with:)
	regexp.MustCompile(`^reloadSections$`),                   // UITableView.reloadSections(_:with:)
	regexp.MustCompile(`^insertRows$`),                       // UITableView.insertRows(at:with:)
	regexp.MustCompile(`^deleteRows$`),                       // UITableView.deleteRows(at:with:)
	regexp.MustCompile(`^insertSections$`),                   // UITableView.insertSections(_:with:)
	regexp.MustCompile(`^deleteSections$`),                   // UITableView.deleteSections(_:with:)
	regexp.MustCompile(`^dequeueReusableCell$`),              // UITableView.dequeueReusableCell(withIdentifier:)
	regexp.MustCompile(`^dequeueReusableSupplementaryView$`), // UICollectionView
	regexp.MustCompile(`^register$`),                         // UITableView/UICollectionView.register(_:)
	regexp.MustCompile(`^beginUpdates$`),                     // UITableView.beginUpdates()
	regexp.MustCompile(`^endUpdates$`),                       // UITableView.endUpdates()
	regexp.MustCompile(`^performBatchUpdates$`),              // UICollectionView.performBatchUpdates(_:)
	regexp.MustCompile(`^scrollToRow$`),                      // UITableView.scrollToRow(at:at:animated:)
	regexp.MustCompile(`^scrollToItem$`),                     // UICollectionView.scrollToItem(at:at:animated:)
	regexp.MustCompile(`^setEditing$`),                       // UIViewController.setEditing(_:animated:)
	regexp.MustCompile(`^becomeFirstResponder$`),             // UIResponder.becomeFirstResponder()
	regexp.MustCompile(`^resignFirstResponder$`),             // UIResponder.resignFirstResponder()
	regexp.MustCompile(`^endEditing$`),                       // UIView.endEditing(_:)
}

func init() {
	dynamicPatternsByLang["swift"] = swiftDynamicPatterns
}
