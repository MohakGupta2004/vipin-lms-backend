-- The diagnostic tests a student can pick from, and their questions.
-- The answer key lives only here: the server scores every attempt and never sends a score to a student.
CREATE TABLE diagnostic_tests (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    slug text NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
    title text NOT NULL,
    description text NOT NULL DEFAULT '',
    exam_id uuid REFERENCES exams(id) ON DELETE SET NULL,
    duration_minutes int NOT NULL CHECK (duration_minutes > 0),
    is_active boolean NOT NULL DEFAULT true,
    sort_order int NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TRIGGER diagnostic_tests_set_updated_at
    BEFORE UPDATE ON diagnostic_tests
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- `number` is the questionId stored in a result's answers. `element` is the chapter (1-9 for CIRE);
-- `answer` indexes into `options`, a JSON array of strings.
CREATE TABLE diagnostic_questions (
    test_id uuid NOT NULL REFERENCES diagnostic_tests(id) ON DELETE CASCADE,
    number int NOT NULL CHECK (number > 0),
    element int NOT NULL CHECK (element > 0),
    question text NOT NULL,
    options jsonb NOT NULL CHECK (jsonb_typeof(options) = 'array' AND jsonb_array_length(options) BETWEEN 2 AND 10),
    answer int NOT NULL,
    PRIMARY KEY (test_id, number),
    CHECK (answer >= 0 AND answer < jsonb_array_length(options))
);

-- RLS is switched on for new tables by the event trigger from migration 32; repeat it here so the
-- answer key stays closed to Supabase's public API even where that trigger is missing.
ALTER TABLE diagnostic_tests ENABLE ROW LEVEL SECURITY;
ALTER TABLE diagnostic_questions ENABLE ROW LEVEL SECURITY;

-- The one diagnostic that existed before tests were listed.
INSERT INTO diagnostic_tests (slug, title, description, exam_id, duration_minutes, sort_order)
VALUES (
    'cire-advanced',
    'CIRE skill diagnostic',
    'Find out which chapters of the exam you already know and which need work.',
    (SELECT id FROM exams WHERE code = 'CIRE'),
    60,
    1
);

INSERT INTO diagnostic_questions (test_id, number, element, question, options, answer)
SELECT (SELECT id FROM diagnostic_tests WHERE slug = 'cire-advanced'), v.number, v.element, v.question, v.options, v.answer
FROM (VALUES
    (1, 1, 'A provincial securities commission is reviewing a new prospectus. Separately, a dealer is investigated for manipulative trading, and a client asks who may protect eligible property if that dealer later becomes insolvent. Which sequence of organizations best matches these three functions?',
     '["Canadian Securities Administrators (CSA); FINTRAC; provincial securities regulator","CIRO; Bank of Canada; Office of the Superintendent of Financial Institutions (OSFI)","Provincial securities regulator; CIRO; Canadian Investor Protection Fund (CIPF)","CIPF; Canadian Depository for Securities (CDS); CIRO"]'::jsonb, 2),
    (2, 1, 'An investment dealer observes repeated transfers among unrelated parties, immediate withdrawals and inconsistent explanations of source of funds. After reviewing the facts, its AML team has reasonable grounds to suspect money laundering. What is the most appropriate next step?',
     '["Wait until law enforcement proves an offence before submitting any report.","Follow the dealer''s escalation process, submit a suspicious transaction report to FINTRAC as soon as practicable, and avoid tipping off the client.","File a report only if the transaction exceeds a fixed dollar threshold.","Inform the client of the suspected offence and require written permission before reporting."]'::jsonb, 1),
    (3, 1, 'A client''s diversified portfolio falls by $18,000 during a market downturn. A month later, the client''s investment dealer becomes insolvent and some securities belonging to clients cannot be returned. Which statement best reflects the purpose of CIPF?',
     '["CIPF only guarantees securities issued by the Government of Canada.","CIPF guarantees the investment dealer''s promised investment returns.","CIPF reimburses both the market decline and missing securities because the dealer held the account.","CIPF may cover eligible missing client property attributable to a member''s insolvency, subject to coverage rules, but does not insure market declines."]'::jsonb, 3),
    (4, 1, 'A new representative confuses regulators with market infrastructure. Which statement correctly distinguishes the entities involved in a Canadian securities transaction?',
     '["CDS supports clearing and settlement of securities, while CIRO oversees investment dealers and applicable trading conduct.","FINTRAC guarantees client accounts when a dealer fails.","OSFI replaces CIRO as the direct regulator of every investment dealer.","OBSI sets exchange trading rules and determines the official price of every trade."]'::jsonb, 0),
    (5, 2, 'An individual investor holds $12 million in securities under administration but has never requested or agreed to be treated as an institutional client. The dealer wants to apply institutional- client exemptions immediately. Which response is most defensible?',
     '["Classify the investor automatically if the investor previously passed the CSC.","Treat the investor as an institutional client because they trade more than once per week.","Classify the investor automatically because holdings exceed $10 million.","Do not treat the asset threshold alone as sufficient; the individual must also request and consent to institutional classification under the applicable definition."]'::jsonb, 3),
    (6, 2, 'A 63-year-old client has substantial net worth and a secure pension, but says they would sell after a 10% portfolio loss. They also need a large cash withdrawal in 18 months. Which KYC conclusion is strongest?',
     '["Financial capacity to bear risk may be relatively high, but low tolerance and a short-term cash need must still shape the risk profile and suitability assessment.","High net worth overrides expressed risk tolerance, so equity-heavy investments are automatically appropriate.","The client''s risk capacity must be low because their stated tolerance is low; the terms are identical.","The pension eliminates the need to assess time horizon and liquidity."]'::jsonb, 0),
    (7, 2, 'A client lists an accountant as a professional adviser and a sibling as a trusted contact person. The sibling calls asking the dealer to sell the client''s investments, and the accountant requests account statements for tax work. What is the best response?',
     '["Send the statements to the accountant automatically because tax professionals have a statutory right of access.","Allow the sibling to trade but decline any information-sharing with the accountant under all circumstances.","Verify the client''s authority and consent before sharing records; neither a trusted contact designation nor the accountant''s role alone gives trading authority.","Execute the sibling''s instructions because trusted contacts can make trading decisions."]'::jsonb, 2),
    (8, 2, 'Two diversified funds offer comparable exposures and expected gross returns. Fund A has annual ongoing costs of 0.40%, Fund B 1.65%. A representative claims fees do not matter because both have the same gross performance outlook. Which response is best?',
     '["Fees matter only when a fund distributes dividends, not when it reinvests returns.","Costs can materially reduce compounded investor returns; the representative must assess them along with risks, features and suitability rather than treating gross returns as net returns.","Both funds will have identical net performance if they hold similar securities.","The higher-fee fund is necessarily unsuitable for every client regardless of other features."]'::jsonb, 1),
    (9, 3, 'An Investment Representative (IR) working on an order desk is asked by a retail client to recommend whether a specific high-yield bond is more suitable than an investment-grade bond. Which action aligns with the role distinction in the syllabus?',
     '["Explain the IR''s role and refer the request for personalized investment advice to an appropriately authorized Registered Representative (RR).","Recommend only the higher-yield bond because the client initiated the inquiry.","Make the recommendation as long as the client has previously completed a risk questionnaire.","Make the recommendation if an RR agrees to check the trade after execution."]'::jsonb, 0),
    (10, 3, 'A self-directed client opens an order-execution-only (OEO) account and asks the platform to select the "best" leveraged ETF for them. Which statement best distinguishes the obligations?',
     '["The OEO service must give a personalized product recommendation to all first-time clients.","The OEO service cannot make a client-specific investment recommendation; its applicable account and operational obligations are distinct from a retail trade-by-trade suitability recommendation.","OEO accounts have no KYC, account-opening or regulatory obligations of any kind.","An OEO service can recommend the best ETF if it displays a one-line risk disclaimer."]'::jsonb, 1),
    (11, 3, 'A dealer adds a complex income product to its approved product list. An RR has read only the marketing summary and wants to recommend it because the product is "dealer-approved." What is still required?',
     '["The client can assume responsibility for KYP by signing a broad risk waiver.","Only historical returns must be checked if the product appears on an approved shelf.","Nothing further: dealer shelf approval automatically satisfies the RR''s KYP obligations.","The RR must understand the product''s structure, features, risks, ongoing costs and relevant impact on the client before making a recommendation."]'::jsonb, 3),
    (12, 3, 'A retail client requests margin financing after changing jobs, taking on a large mortgage and reducing their emergency savings. The representative plans to rely on a risk questionnaire completed two years ago. What should happen first?',
     '["Assess whether the margin agreement is signed; that alone establishes suitability.","Approve margin automatically because the earlier questionnaire remains on file.","Update relevant client information and evaluate account appropriateness, financing strategy and suitability under current circumstances before proceeding.","Change only the client''s occupation because the other facts concern banking rather than investments."]'::jsonb, 2),
    (13, 3, 'A dealer serves a regulated institutional client whose treasury team routinely trades government bonds but has never used complex equity derivatives. The client requests an unfamiliar derivative structure. What is the strongest compliance analysis?',
     '["Institutional clients must always receive identical suitability treatment to retail clients.","Institutional classification means the dealer can presume sophistication in all investment products.","The existence of an ISDA agreement automatically proves the team understands every derivative.","Assess the client''s sophistication and ability to understand the transaction in the context of that product; institutional status alone does not establish expertise in every instrument."]'::jsonb, 3),
    (14, 4, 'A client alleges an unauthorized trade. The adviser offers to personally reimburse the loss if the client agrees not to notify the dealer or CIRO. Which is the most appropriate response?',
     '["Accept the settlement if the client confirms satisfaction in writing.","Treat the matter as resolved and omit it from firm records because no regulator has contacted the adviser.","Refer the complaint through the dealer''s required process; the adviser should not privately settle it outside the firm''s approval and reporting controls.","Wait until the client hires a lawyer before notifying the dealer."]'::jsonb, 2),
    (15, 4, 'A client has pursued an investment-related complaint with their dealer and has not received a substantive final response after 90 days. The client wants an independent avenue to seek resolution. Which is most appropriate?',
     '["Ask CIPF to determine compensation for poor investment advice, regardless of dealer insolvency.","The client may contact the Ombudsman for Banking Services and Investments (OBSI), subject to the applicable process and eligibility rules.","Assume CIRO automatically reimburses the client for any alleged loss.","Require FINTRAC to arbitrate the investment dispute."]'::jsonb, 1),
    (16, 5, 'To reduce inflation, the Bank of Canada unexpectedly raises policy rates. Assuming other factors are constant, which short-term market reaction is most consistent with the economic relationships in the CIRE syllabus?',
     '["Borrowing costs tend to rise, existing fixed-coupon bond prices tend to fall and interest-sensitive demand may weaken.","The monetary policy change has no bearing on the economy or securities valuations.","Borrowing costs fall while existing fixed-coupon bond prices rise.","Higher policy rates automatically increase the price of every equity security."]'::jsonb, 0),
    (17, 5, 'An issuer reports higher earnings, but operating cash flow has declined, receivables are rising faster than sales and the notes describe looser customer credit terms. What is the most defensible analytical conclusion?',
     '["Reported earnings growth warrants further scrutiny because collection risk and earnings-to-cash conversion may be deteriorating.","The income statement is irrelevant whenever cash flow is lower than net income.","The issuer must be insolvent because receivables increased.","Higher net income proves that liquidity and cash collections improved."]'::jsonb, 0),
    (18, 5, 'Canada imports more goods and services and receives less net investment income from abroad than it exports or earns, producing a current-account deficit. Ignoring measurement errors, how is this deficit ordinarily financed in balance-of-payments accounting?',
     '["By forcing the current account to become a surplus immediately.","By eliminating all foreign-exchange transactions for the period.","By requiring domestic companies to stop issuing securities.","By a corresponding net financial inflow or reduction in external assets, reflected in the capital and financial accounts."]'::jsonb, 3),
    (19, 6, 'A client submits an immediate-or-cancel (IOC) limit order to buy 1,000 shares at $48 or less. Only 300 shares are available at $47.90, with all remaining offers above $48. Assume no further liquidity arrives. What is the expected outcome?',
     '["Buy 300 shares at an eligible price and immediately cancel the unfilled 700 shares.","Buy 300 shares and leave the remaining 700 in the order book until the client cancels them.","Cancel the entire order because an IOC order cannot be partially executed.","Buy all 1,000 shares because the order is marked immediate."]'::jsonb, 0),
    (20, 6, 'A trading-desk employee learns that a large institutional client is about to submit a confidential buy order likely to move a thin stock. Before the client order is entered, the employee buys shares in their personal account. What is the primary concern?',
     '["Permissible personal investing because the client order has not yet executed.","A purely tax-related matter unrelated to market integrity.","Potential front-running and misuse of confidential order information requiring compliance escalation.","Acceptable conduct if the employee sells the shares after the institutional order fills."]'::jsonb, 2),
    (21, 6, 'A dealer notices a client repeatedly arranging trades between apparently related accounts at unusual prices, with little change in beneficial ownership but sharply higher reported volume. What is the best immediate response?',
     '["Publish the client''s identity to other market participants to warn them.","Treat the pattern as a possible manipulative or deceptive activity and promptly escalate through the firm''s gatekeeping controls for review.","Automatically refund every commission without reviewing the trading pattern.","Ignore it because a formal court finding of manipulation is required before escalation."]'::jsonb, 1),
    (22, 6, 'A retail client purchases exchange-listed shares through an investment dealer. Which description correctly links the dealer''s execution and post-trade responsibilities?',
     '["A client trade is complete only when CIPF approves its investment merits.","An order confirmation does not need to identify charges because they can be disclosed only at year-end.","The dealer handles execution and confirmation under applicable rules, including required transaction details; settlement and delivery involve relevant market infrastructure.","The issuer directly confirms all secondary-market trades and remits commissions to the client."]'::jsonb, 2),
    (23, 7, 'A conventional bond has a $1,000 face value, a 4% annual coupon and a current market price of $920. Assuming it is ultimately redeemed at par and there is no default, which statement is most accurate?',
     '["Its yield to maturity is zero because the bond trades below face value.","Its current yield is about 4.35%, while its yield to maturity is generally higher than its current yield.","Its current yield is about 3.68%, and yield to maturity is below the coupon.","Its current yield is exactly 4.00%, and its yield to maturity must also equal 4.00%."]'::jsonb, 1),
    (24, 7, 'Two investment-grade bonds have similar credit quality and yield, but Bond X has a modified duration of 2.1 and Bond Y has a modified duration of 7.4. For the same small upward yield change, which conclusion is most defensible?',
     '["Duration measures credit default probability, not interest-rate sensitivity.","Both bonds must show precisely the same percentage move because their yields are similar.","Bond Y would generally experience the greater percentage price decline because its duration is higher.","Bond X must suffer the larger price decline because a lower duration means greater rate sensitivity."]'::jsonb, 2),
    (25, 7, 'A solvent company suspends its preferred-share dividend after a poor year. An investor argues that preferred shares are risk-free because "preferred" means a legally guaranteed payment. Which response is accurate?',
     '["Preferred shares are economically identical to Treasury bills.","Preferred shareholders rank ahead of all secured creditors when a company is wound up.","Preferred shares generally rank ahead of common equity for dividends and liquidation claims, but remain equity and dividends are not universally guaranteed.","A preferred dividend is always as contractually unavoidable as interest on senior debt."]'::jsonb, 2),
    (26, 7, 'An index is calculated using share prices rather than companies'' total market capitalizations. One constituent trades at $240 per share and another at $30 per share. Which consequence is most consistent with a price-weighted design?',
     '["A given percentage price movement in the higher-priced stock generally has a greater index effect before index adjustments.","The stock with the larger number of shares outstanding necessarily has the greater index weight.","Both stocks have identical influence because their percentage price changes are identical.","Price-weighted indices automatically exclude companies that split their shares."]'::jsonb, 0),
    (27, 7, 'An investor places $50,000 in a mutual fund. For a simplified one-year estimate, assume the portfolio earns 5.0% before recurring fund expenses and the fund''s ongoing expenses reduce return by 1.8 percentage points. Ignore taxes and sales charges. Which ending value is closest?',
     '["$51,600","$53,400","$50,900","$52,500"]'::jsonb, 0),
    (28, 7, 'An ETF has an estimated NAV of $19.70, but its displayed bid and ask are $19.60 and $19.98 during volatile trading. A client wants 2,000 units immediately. What should the representative explain?',
     '["A wide bid-ask spread proves the fund manager has misappropriated assets.","A market buy order is guaranteed to execute at the reported NAV.","An ETF''s NAV and every executed trade price must always be equal.","ETF market prices can differ from NAV, spreads can widen and a limit order can control the maximum purchase price, though it may not fill."]'::jsonb, 3),
    (29, 7, 'A structured note advertises return of principal at maturity, conditional on the issuer remaining able to meet its obligations. A client calls this the same as a risk-free government deposit. What is the most important correction?',
     '["Issuer creditworthiness becomes irrelevant once a note is sold to an investor.","The word \"principal\" guarantees full liquidity at any price before maturity.","Principal-protected structured notes always qualify for CDIC coverage regardless of structure.","A principal-protection feature can be subject to issuer credit risk and product terms, and it need not protect an investor who sells before maturity."]'::jsonb, 3),
    (30, 8, 'A client already owns 1,000 shares and expects long-term upside but wants a defined downside floor for the next three months without selling the stock. Which basic derivatives position best matches this objective?',
     '["Buy additional shares on margin to offset a potential decline.","Purchase put options on the shares, subject to the option premium and contract terms.","Write uncovered call options to maximize downside protection.","Write call options without owning an offsetting position to create a guaranteed floor."]'::jsonb, 1),
    (31, 8, 'A client owns 100 shares and wants modest premium income, accepts that gains above a selected strike may be forfeited, and understands assignment risk. Which listed-options strategy best fits this stated trade-off?',
     '["Buy a protective put and expect to receive option premium immediately.","Sell a naked put and call simultaneously while claiming no collateral or margin exposure.","Write one call option covered by the existing shares, following account approval and applicable derivatives documentation requirements.","Buy a long straddle to guarantee premium income in a flat market."]'::jsonb, 2),
    (32, 9, 'An investment dealer receives higher compensation for selling an affiliated fund than a comparable third-party fund. An RR proposes recommending the affiliated fund solely because the commission is higher, with a short disclosure note. Which is the strongest interpretation?',
     '["The recommendation is automatically appropriate if the fund fact sheet includes the fee.","The highest-compensation product must be recommended to ensure the dealer remains profitable.","Conflicts of interest do not apply to affiliated products that pass the dealer''s due diligence.","A material conflict must be identified and addressed in the client''s best interest; disclosure by itself does not justify a compensation-driven recommendation."]'::jsonb, 3),
    (33, 9, 'An RR plans to run paid investment webinars on weekends through a personal company. The RR says no dealer approval is needed because the sessions are outside business hours and do not mention the employer. What is the most appropriate action?',
     '["Avoid telling the dealer unless a webinar attendee files a complaint.","Disclose the proposed outside activity to the dealer and follow its due-diligence, approval and supervision process before undertaking it.","Use the dealer''s confidential client list to promote the webinars, provided no trading advice is given.","Begin the activity immediately because outside employment cannot create securities-related conflicts."]'::jsonb, 1),
    (34, 9, 'A client offers to lend their RR $25,000 privately to finance a vacation, in exchange for interest above the client''s savings-account rate. The RR is tempted because both sides appear willing. What is the best response?',
     '["Accept if the interest rate is at least the prevailing prime rate.","Accept if the RR promises not to recommend new products while the loan is outstanding.","Accept if the client records the transaction in a private note, without dealer involvement.","Decline the personal borrowing arrangement and follow the dealer''s policies and applicable restrictions on personal financial dealings with clients."]'::jsonb, 3),
    (35, 9, 'An investment-banking colleague accidentally sends an RR confidential, non-public details of an issuer''s forthcoming acquisition. The RR believes clients could benefit from buying before the announcement. Which action is most appropriate?',
     '["Do not trade or communicate the information for client benefit; preserve confidentiality and follow the dealer''s information-barrier and escalation procedures.","Wait until the acquisition story appears on an unverified online forum, then trade based on the email.","Buy only in managed accounts, since the RR is acting for clients rather than personally.","Share the information with sophisticated clients, who can independently decide whether to trade."]'::jsonb, 0),
    (36, 9, 'An RR accidentally receives a file containing account balances and identification information belonging to another branch''s clients. A colleague suggests using the list to market products and deleting it afterward. What is the best course?',
     '["Use the data only for clients with large balances because the marketing purpose is beneficial.","Do not use the information; secure it and promptly report the incident through the dealer''s privacy and information-security procedures.","Send the file to a personal email account to determine whether the records are useful.","Discard the file silently because an accidental disclosure can never be a reportable incident."]'::jsonb, 1)
) AS v (number, element, question, options, answer);

-- Every result now belongs to a test. Existing results are all from the CIRE diagnostic.
ALTER TABLE diagnostic_results ADD COLUMN test_id uuid REFERENCES diagnostic_tests(id) ON DELETE RESTRICT;
UPDATE diagnostic_results SET test_id = (SELECT id FROM diagnostic_tests WHERE slug = 'cire-advanced');
ALTER TABLE diagnostic_results ALTER COLUMN test_id SET NOT NULL;

-- A user can take each test once, instead of one diagnostic in all.
DROP INDEX diagnostic_results_user_unique;
CREATE UNIQUE INDEX diagnostic_results_user_test_unique ON diagnostic_results (user_id, test_id);
